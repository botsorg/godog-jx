package cmd

import (
	"fmt"
	"net/url"

	"github.com/jenkins-x/jx/pkg/auth"
	"github.com/jenkins-x/jx/pkg/gits"
	"github.com/jenkins-x/jx/pkg/jenkins"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/jenkins-x/jx/pkg/util"
	corev1 "k8s.io/api/core/v1"
)

// ImportProject imports a MultiBranchProject into Jenkins for the given git URL
func (o *CommonOptions) ImportProject(gitURL string, dir string, jenkinsfile string, branchPattern, credentials string, failIfExists bool, gitProvider gits.GitProvider, authConfigSvc auth.AuthConfigService, isEnvironment bool, batchMode bool) error {

	jenk, err := o.JenkinsClient()
	if err != nil {
		return err
	}

	secrets, err := o.Factory.LoadPipelineSecrets(kube.ValueKindGit, "")
	if err != nil {
		return err
	}

	if gitURL == "" {
		return fmt.Errorf("No Git repository URL found!")
	}
	gitInfo, err := gits.ParseGitURL(gitURL)
	if err != nil {
		return fmt.Errorf("Failed to parse git URL %s due to: %s", gitURL, err)
	}

	if branchPattern == "" {
		o.Printf("Querying if the repo is a fork at %s with kind %s\n", gitProvider.ServerURL(), gitProvider.Kind())
		fork, err := gits.GitIsFork(gitProvider, gitInfo, dir)
		if err != nil {
			return fmt.Errorf("No branch pattern specified and could not determine if the git repository is a fork: %s", err)
		}
		if fork {
			// lets figure out which branches to enable for a fork
			branch, err := gits.GitGetBranch(dir)
			if err != nil {
				return fmt.Errorf("Failed to get current branch in dir %s: %s", dir, err)
			}
			if branch == "" {
				return fmt.Errorf("Failed to get current branch in dir %s", dir)
			}
			// TODO do we need to scape any wacky characters to make it a valid branch pattern?
			branchPattern = branch
			o.Printf("No branch pattern specified and this repository appears to be a fork so defaulting the branch patterns to run CI/CD on to: %s\n", branchPattern)
		} else {
			branchPattern = jenkins.BranchPattern(gitProvider.Kind())
		}
	}

	createCredential := true
	if credentials == "" {
		// lets try find the credentials from the secrets
		credentials = findGitCredentials(gitProvider, secrets)
		if credentials != "" {
			createCredential = false
		}
	}
	if credentials == "" {
		// TODO lets prompt the user to add a new credential for the git provider...
		config := authConfigSvc.Config()
		u := gitInfo.HostURL()
		server := config.GetOrCreateServer(u)
		if len(server.Users) == 0 {
			// lets check if the host was used in `~/.jx/gitAuth.yaml` instead of URL
			s2 := config.GetOrCreateServer(gitInfo.Host)
			if s2 != nil && len(s2.Users) > 0 {
				server = s2
				u = gitInfo.Host
			}
		}
		user, err := config.PickServerUserAuth(server, "user name for the Jenkins Pipeline", batchMode)
		if err != nil {
			return err
		}
		if user.Username == "" {
			return fmt.Errorf("Could find a username for git server %s", u)
		}

		credentials, err = o.updatePipelineGitCredentialsSecret(server, user)
		if err != nil {
			return err
		}

		if credentials == "" {
			fmt.Errorf("Failed to find the created pipeline secret for the server %s", server.URL)
		} else {
			createCredential = false
		}
	}
	if createCredential {
		_, err = jenk.GetCredential(credentials)
		if err != nil {
			config := authConfigSvc.Config()
			u := gitInfo.HostURL()
			server := config.GetOrCreateServer(u)
			if len(server.Users) == 0 {
				// lets check if the host was used in `~/.jx/gitAuth.yaml` instead of URL
				s2 := config.GetOrCreateServer(gitInfo.Host)
				if s2 != nil && len(s2.Users) > 0 {
					server = s2
					u = gitInfo.Host
				}
			}
			user, err := config.PickServerUserAuth(server, "user name for the Jenkins Pipeline", batchMode)
			if err != nil {
				return err
			}
			if user.Username == "" {
				return fmt.Errorf("Could find a username for git server %s", u)
			}
			err = jenk.CreateCredential(credentials, user.Username, user.ApiToken)

			if err != nil {
				return fmt.Errorf("error creating jenkins credential %s at %s %v", credentials, jenk.BaseURL(), err)
			}
			o.Printf("Created credential %s for host %s user %s\n", util.ColorInfo(credentials), util.ColorInfo(u), util.ColorInfo(user.Username))
		}
	}
	org := gitInfo.Organisation
	folder, err := jenk.GetJob(org)
	if err != nil {
		// could not find folder so lets try create it
		jobUrl := util.UrlJoin(jenk.BaseURL(), jenk.GetJobURLPath(org))
		folderXml := jenkins.CreateFolderXml(jobUrl, org)
		//o.Printf("XML: %s\n", folderXml)
		err = jenk.CreateJobWithXML(folderXml, org)
		if err != nil {
			return fmt.Errorf("Failed to create the %s folder in jenkins: %s", org, err)
		}
		//o.Printf("Created Jenkins folder: %s\n", org)
	} else {
		c := folder.Class
		if c != "com.cloudbees.hudson.plugins.folder.Folder" {
			o.Printf("Warning the folder %s is of class %s", org, c)
		}
	}
	projectXml := jenkins.CreateMultiBranchProjectXml(gitInfo, gitProvider, credentials, branchPattern, jenkinsfile)
	jobName := gitInfo.Name
	job, err := jenk.GetJobByPath(org, jobName)
	if err == nil {
		if failIfExists {
			return fmt.Errorf("Job already exists in Jenkins at %s", job.Url)
		} else {
			o.Printf("Job already exists in Jenkins at %s\n", job.Url)
		}
	} else {
		//o.Printf("Creating MultiBranchProject %s from XML: %s\n", jobName, projectXml)
		err = jenk.CreateFolderJobWithXML(projectXml, org, jobName)
		if err != nil {
			return fmt.Errorf("Failed to create MultiBranchProject job %s in folder %s due to: %s", jobName, org, err)
		}
		job, err = jenk.GetJobByPath(org, jobName)
		if err != nil {
			return fmt.Errorf("Failed to find the MultiBranchProject job %s in folder %s due to: %s", jobName, org, err)
		}
		o.Printf("Created Jenkins Project: %s\n", util.ColorInfo(job.Url))
		o.Printf("\n")
		if !isEnvironment {
			o.Printf("Watch pipeline activity via:    %s\n", util.ColorInfo(fmt.Sprintf("jx get activity -f %s -w", gitInfo.Name)))
			o.Printf("Browse the pipeline log via:    %s\n", util.ColorInfo(fmt.Sprintf("jx get build logs %s", gitInfo.PipelinePath())))
			o.Printf("Open the Jenkins console via    %s\n", util.ColorInfo("jx console"))
			o.Printf("You can list the pipelines via: %s\n", util.ColorInfo("jx get pipelines"))
			o.Printf("When the pipeline is complete:  %s\n", util.ColorInfo("jx get applications"))
			o.Printf("\n")
			o.Printf("For more help on available commands see: %s\n", util.ColorInfo("http://jenkins-x.io/developing/browsing/"))
			o.Printf("\n")
		}
		o.Printf(util.ColorStatus("Note that your first pipeline may take a few minutes to start while the necessary docker images get downloaded!\n\n"))

		params := url.Values{}
		err = jenk.Build(job, params)
		if err != nil {
			return fmt.Errorf("Failed to trigger job %s due to %s", job.Url, err)
		}

	}

	// register the webhook
	suffix := gitProvider.JenkinsWebHookPath(gitURL, "")
	webhookUrl := util.UrlJoin(jenk.BaseURL(), suffix)
	webhook := &gits.GitWebHookArguments{
		Owner: gitInfo.Organisation,
		Repo:  gitInfo.Name,
		URL:   webhookUrl,
	}
	return gitProvider.CreateWebHook(webhook)
}

// findGitCredentials finds the credential name from the pipeline git Secrets
func findGitCredentials(gitProvider gits.GitProvider, secrets *corev1.SecretList) string {
	if secrets == nil {
		return ""
	}
	u := gitProvider.ServerURL()
	for _, secret := range secrets.Items {
		annotations := secret.Annotations
		if annotations != nil {
			gitUrl := annotations[kube.AnnotationURL]
			if u == gitUrl {
				return secret.Name
			}
		}
	}
	return ""
}
