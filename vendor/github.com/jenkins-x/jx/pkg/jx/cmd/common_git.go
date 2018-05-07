package cmd

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/jenkins-x/jx/pkg/auth"
	"github.com/jenkins-x/jx/pkg/gits"
	"github.com/jenkins-x/jx/pkg/issues"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/spf13/cobra"
	gitcfg "gopkg.in/src-d/go-git.v4/config"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// createGitProvider creates a git from the given directory
func (o *CommonOptions) createGitProvider(dir string) (*gits.GitRepositoryInfo, gits.GitProvider, issues.IssueProvider, error) {
	gitDir, gitConfDir, err := gits.FindGitConfigDir(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	if gitDir == "" || gitConfDir == "" {
		o.warnf("No git directory could be found from dir %s\n", dir)
		return nil, nil, nil, nil
	}

	gitUrl, err := gits.DiscoverUpstreamGitURL(gitConfDir)
	if err != nil {
		return nil, nil, nil, err
	}
	gitInfo, err := gits.ParseGitURL(gitUrl)
	if err != nil {
		return nil, nil, nil, err
	}
	authConfigSvc, err := o.Factory.CreateGitAuthConfigService()
	if err != nil {
		return gitInfo, nil, nil, err
	}
	gitKind, err := o.GitServerKind(gitInfo)
	gitProvider, err := gitInfo.CreateProvider(authConfigSvc, gitKind)
	if err != nil {
		return gitInfo, gitProvider, nil, err
	}
	tracker, err := o.createIssueProvider(dir)
	if err != nil {
		return gitInfo, gitProvider, tracker, err
	}
	return gitInfo, gitProvider, tracker, nil
}

func (o *CommonOptions) updatePipelineGitCredentialsSecret(server *auth.AuthServer, userAuth *auth.UserAuth) (string, error) {
	client, curNs, err := o.Factory.CreateClient()
	if err != nil {
		return "", err
	}
	ns, _, err := kube.GetDevNamespace(client, curNs)
	if err != nil {
		return "", err
	}
	options := metav1.GetOptions{}
	serverName := server.Name
	name := kube.ToValidName(kube.SecretJenkinsPipelineGitCredentials + server.Kind + "-" + serverName)
	secrets := client.CoreV1().Secrets(ns)
	secret, err := secrets.Get(name, options)
	create := false
	operation := "update"
	labels := map[string]string{
		kube.LabelCredentialsType: kube.ValueCredentialTypeUsernamePassword,
		kube.LabelCreatedBy:       kube.ValueCreatedByJX,
		kube.LabelKind:            kube.ValueKindGit,
		kube.LabelServiceKind:     server.Kind,
	}
	annotations := map[string]string{
		kube.AnnotationCredentialsDescription: fmt.Sprintf("API Token for acccessing %s git service inside pipelines", server.URL),
		kube.AnnotationURL:                    server.URL,
		kube.AnnotationName:                   serverName,
	}
	if err != nil {
		// lets create a new secret
		create = true
		operation = "create"
		secret = &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: annotations,
				Labels:      labels,
			},
			Data: map[string][]byte{},
		}
	} else {
		secret.Annotations = kube.MergeMaps(secret.Annotations, annotations)
		secret.Labels = kube.MergeMaps(secret.Labels, labels)
	}
	if userAuth.Username != "" {
		secret.Data["username"] = []byte(userAuth.Username)
	}
	if userAuth.ApiToken != "" {
		secret.Data["password"] = []byte(userAuth.ApiToken)
	}
	if create {
		_, err = secrets.Create(secret)
	} else {
		_, err = secrets.Update(secret)
	}
	if err != nil {
		return name, fmt.Errorf("Failed to %s secret %s due to %s", operation, secret.Name, err)
	}

	cm, err := client.CoreV1().ConfigMaps(ns).Get(kube.ConfigMapJenkinsX, metav1.GetOptions{})
	if err != nil {
		return name, fmt.Errorf("Could not load Jenkins ConfigMap: %s", err)
	}

	updated, err := kube.UpdateJenkinsGitServers(cm, server, userAuth, name)
	if err != nil {
		return name, err
	}
	if updated {
		_, err = client.CoreV1().ConfigMaps(ns).Update(cm)
		if err != nil {
			return name, fmt.Errorf("Failed to update Jenkins ConfigMap: %s", err)
		}
		o.Printf("Updated the Jenkins ConfigMap %s\n", kube.ConfigMapJenkinsX)

		// wait a little bit to give k8s chance to sync the ConfigMap to the file system
		time.Sleep(time.Second * 2)

		// lets ensure that the git server + credential is in the Jenkins server configuration
		jenk, err := o.JenkinsClient()
		if err != nil {
			return name, err
		}
		// TODO reload does not seem to reload the plugin content
		//err = jenk.Reload()
		err = jenk.SafeRestart()
		if err != nil {
			o.warnf("Failed to safe restart Jenkins after configuration change %s\n", err)
		} else {
			o.Printf("Safe Restarted Jenkins server\n")
		}
	}

	return name, nil
}

func (o *CommonOptions) ensureGitServiceCRD(server *auth.AuthServer) error {
	kind := server.Kind
	if kind == "" || kind == "github" || server.URL == "" {
		return nil
	}
	apisClient, err := o.Factory.CreateApiExtensionsClient()
	if err != nil {
		return err
	}
	err = kube.RegisterGitServiceCRD(apisClient)
	if err != nil {
		return err
	}

	jxClient, devNs, err := o.JXClientAndDevNamespace()
	if err != nil {
		return err
	}
	return kube.EnsureGitServiceExistsForHost(jxClient, devNs, kind, server.Name, server.URL, o.Out)
}

func (o *CommonOptions) discoverGitURL(gitConf string) (string, error) {
	if gitConf == "" {
		return "", fmt.Errorf("No GitConfDir defined!")
	}
	cfg := gitcfg.NewConfig()
	data, err := ioutil.ReadFile(gitConf)
	if err != nil {
		return "", fmt.Errorf("Failed to load %s due to %s", gitConf, err)
	}

	err = cfg.Unmarshal(data)
	if err != nil {
		return "", fmt.Errorf("Failed to unmarshal %s due to %s", gitConf, err)
	}
	remotes := cfg.Remotes
	if len(remotes) == 0 {
		return "", nil
	}
	url := gits.GetRemoteUrl(cfg, "origin")
	if url == "" {
		url = gits.GetRemoteUrl(cfg, "upstream")
		if url == "" {
			url, err = o.pickRemoteURL(cfg)
			if err != nil {
				return "", err
			}
		}
	}
	return url, nil
}

func addGitRepoOptionsArguments(cmd *cobra.Command, repositoryOptions *gits.GitRepositoryOptions) {
	cmd.Flags().StringVarP(&repositoryOptions.ServerURL, "git-provider-url", "", "", "The git server URL to create new git repositories inside")
	cmd.Flags().StringVarP(&repositoryOptions.Username, "git-username", "", "", "The git username to use for creating new git repositories")
	cmd.Flags().StringVarP(&repositoryOptions.ApiToken, "git-api-token", "", "", "The git API token to use for creating new git repositories")
}

func (o *CommonOptions) GitServerKind(gitInfo *gits.GitRepositoryInfo) (string, error) {
	return o.GitServerHostURLKind(gitInfo.HostURL())
}

func (o *CommonOptions) GitServerHostURLKind(hostURL string) (string, error) {
	jxClient, devNs, err := o.JXClientAndDevNamespace()
	if err != nil {
		return "", err
	}

	kubeClient, _, err := o.KubeClient()
	if err != nil {
		return "", err
	}

	apisClient, err := o.Factory.CreateApiExtensionsClient()
	if err != nil {
		return "", err
	}
	err = kube.RegisterGitServiceCRD(apisClient)
	if err != nil {
		return "", err
	}

	kind, err := kube.GetGitServiceKind(jxClient, kubeClient, devNs, hostURL)
	if err != nil {
		return kind, err
	}
	if kind == "" {
		if o.BatchMode {
			return "", fmt.Errorf("No git server kind could be found for URL %s\nPlease try specify it via: jx create git server someKind %s", hostURL, hostURL)
		}
		kind, err = util.PickName(gits.KindGits, "Pick what kind of git server is")
		if err != nil {
			return "", err
		}
		if kind == "" {
			return "", fmt.Errorf("No git kind chosen!")
		}
	}
	return kind, nil
}

// gitProviderForURL returns a GitProvider for the given git URL
func (o *CommonOptions) gitProviderForURL(gitURL string, message string) (gits.GitProvider, error) {
	gitInfo, err := gits.ParseGitURL(gitURL)
	if err != nil {
		return nil, err
	}
	authConfigSvc, err := o.Factory.CreateGitAuthConfigService()
	if err != nil {
		return nil, err
	}
	gitKind, err := o.GitServerKind(gitInfo)
	if err != nil {
		return nil, err
	}
	return gitInfo.PickOrCreateProvider(authConfigSvc, message, o.BatchMode, gitKind)
}
