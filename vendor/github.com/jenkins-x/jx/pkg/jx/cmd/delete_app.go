package cmd

import (
	"fmt"
	"io"
	"os/user"
	"strings"
	"time"

	"github.com/jenkins-x/golang-jenkins"
	"github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	"github.com/jenkins-x/jx/pkg/helm"
	"github.com/jenkins-x/jx/pkg/jenkins"
	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/spf13/cobra"

	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/jenkins-x/jx/pkg/util"
)

var (
	deleteAppLong = templates.LongDesc(`
		Deletes one or more Applications from Jenkins

		Note that this command does not remove the underlying Git Repositories. 

		For that see the [jx delete repo](http://jenkins-x.io/commands/jx_delete_repo/) command.

`)

	deleteAppExample = templates.Examples(`
		# prompt for the available apps to delete
		jx delete app 

		# delete a specific app 
		jx delete app cheese
	`)
)

// DeleteAppOptions are the flags for this delete commands
type DeleteAppOptions struct {
	CommonOptions

	SelectAll           bool
	SelectFilter        string
	IgnoreEnvironments  bool
	NoMergePullRequest  bool
	Timeout             string
	PullRequestPollTime string

	// calculated fields
	TimeoutDuration         *time.Duration
	PullRequestPollDuration *time.Duration
}

// NewCmdDeleteApp creates a command object for this command
func NewCmdDeleteApp(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &DeleteAppOptions{
		CommonOptions: CommonOptions{
			Factory: f,
			Out:     out,
			Err:     errOut,
		},
	}

	cmd := &cobra.Command{
		Use:     "application",
		Short:   "Deletes one or many applications from Jenkins",
		Long:    deleteAppLong,
		Example: deleteAppExample,
		Aliases: []string{"applications", "app", "apps"},
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}
	cmd.Flags().BoolVarP(&options.IgnoreEnvironments, "no-env", "", false, "Do not remove the app from any of the Environments")
	cmd.Flags().BoolVarP(&options.SelectAll, "all", "a", false, "Selects all the matched apps")
	cmd.Flags().BoolVarP(&options.NoMergePullRequest, "no-merge", "", false, "Disables automatic merge of promote Pull Requests")
	cmd.Flags().StringVarP(&options.SelectFilter, "filter", "f", "", "Filter the list of apps to those containing this text")
	cmd.Flags().StringVarP(&options.Timeout, optionTimeout, "t", "1h", "The timeout to wait for the promotion to succeed in the underlying Environment. The command fails if the timeout is exceeded or the promotion does not complete")
	cmd.Flags().StringVarP(&options.PullRequestPollTime, optionPullRequestPollTime, "", "20s", "Poll time when waiting for a Pull Request to merge")

	return cmd
}

// Run implements this command
func (o *DeleteAppOptions) Run() error {
	args := o.Args

	jenk, err := o.JenkinsClient()
	if err != nil {
		return err
	}

	jobs, err := jenkins.LoadAllJenkinsJobs(jenk)
	if err != nil {
		return err
	}

	names := []string{}
	m := map[string]*gojenkins.Job{}

	for _, j := range jobs {
		if jenkins.IsMultiBranchProject(j) {
			name := j.FullName
			names = append(names, name)
			m[name] = j
		}
	}

	if o.PullRequestPollTime != "" {
		duration, err := time.ParseDuration(o.PullRequestPollTime)
		if err != nil {
			return fmt.Errorf("Invalid duration format %s for option --%s: %s", o.PullRequestPollTime, optionPullRequestPollTime, err)
		}
		o.PullRequestPollDuration = &duration
	}
	if o.Timeout != "" {
		duration, err := time.ParseDuration(o.Timeout)
		if err != nil {
			return fmt.Errorf("Invalid duration format %s for option --%s: %s", o.Timeout, optionTimeout, err)
		}
		o.TimeoutDuration = &duration
	}

	if len(names) == 0 {
		return fmt.Errorf("There are no Apps in Jenkins")
	}

	if len(args) == 0 {
		args, err = util.SelectNamesWithFilter(names, "Pick Applications to remove from Jenkins:", o.SelectAll, o.SelectFilter)
		if err != nil {
			return err
		}
	} else {
		for _, arg := range args {
			if util.StringArrayIndex(names, arg) < 0 {
				return util.InvalidArg(arg, names)
			}
		}
	}
	if len(args) == 0 {
		return fmt.Errorf("There are no Apps in Jenkins")
	}
	deleteMessage := strings.Join(args, ", ")

	if !util.Confirm("You are about to delete these Applications from Jenkins: "+deleteMessage, false, "The list of Applications names to be deleted from Jenkins") {
		return nil
	}
	for _, name := range args {
		job := m[name]
		if job != nil {
			err = o.deleteApp(jenk, name, job)
			if err != nil {
				return err
			}
		}
	}
	o.Printf("Deleted Applications %s\n", util.ColorInfo(deleteMessage))
	return nil
}

func (o *DeleteAppOptions) deleteApp(jenkinsClient *gojenkins.Jenkins, name string, job *gojenkins.Job) error {
	apisClient, err := o.Factory.CreateApiExtensionsClient()
	if err != nil {
		return err
	}
	err = kube.RegisterEnvironmentCRD(apisClient)
	if err != nil {
		return err
	}

	jxClient, ns, err := o.JXClientAndDevNamespace()
	if err != nil {
		return err
	}
	envMap, envNames, err := kube.GetOrderedEnvironments(jxClient, ns)
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}

	appName := o.appNameFromJenkinsJobName(name)
	for _, envName := range envNames {
		// TODO filter on environment names?
		env := envMap[envName]
		if env != nil {
			err = o.deleteAppFromEnvironment(env, appName, u.Username)
			if err != nil {
				return err
			}
		}
	}

	// lets try delete the job from each environment first
	return jenkinsClient.DeleteJob(*job)
}

func (o *DeleteAppOptions) appNameFromJenkinsJobName(name string) string {
	path := strings.Split(name, "/")
	return path[len(path)-1]
}

func (o *DeleteAppOptions) deleteAppFromEnvironment(env *v1.Environment, appName string, username string) error {
	if env.Spec.Source.URL == "" {
		return nil
	}
	o.Printf("Removing app %s from environment %s\n", appName, env.Spec.Label)

	branchName := "delete-" + appName
	title := "Delete application " + appName + " from this environment"
	message := "The command `jx delete app` was run by " + username + " and it generated this Pull Request"

	modifyRequirementsFn := func(requirements *helm.Requirements) error {
		requirements.RemoveApp(appName)
		return nil
	}
	info, err := o.createEnvironmentPullRequest(env, modifyRequirementsFn, branchName, title, message)
	if err != nil {
		return err
	}

	duration := *o.TimeoutDuration
	end := time.Now().Add(duration)

	return o.waitForGitOpsPullRequest(env, info, end, duration)
}

func (o *DeleteAppOptions) waitForGitOpsPullRequest(env *v1.Environment, pullRequestInfo *ReleasePullRequestInfo, end time.Time, duration time.Duration) error {
	if pullRequestInfo != nil {
		logMergeFailure := false
		pr := pullRequestInfo.PullRequest
		o.Printf("Waiting for pull request %s to merge\n", pr.URL)

		for {
			gitProvider := pullRequestInfo.GitProvider
			err := gitProvider.UpdatePullRequestStatus(pr)
			if err != nil {
				return fmt.Errorf("Failed to query the Pull Request status for %s %s", pr.URL, err)
			}

			if pr.Merged != nil && *pr.Merged {
				o.Printf("Pull Request %s is merged!\n", util.ColorInfo(pr.URL))
				return nil
			} else {
				if pr.IsClosed() {
					o.warnf("Pull Request %s is closed\n", util.ColorInfo(pr.URL))
					return fmt.Errorf("Promotion failed as Pull Request %s is closed without merging", pr.URL)
				}
				// lets try merge if the status is good
				status, err := gitProvider.PullRequestLastCommitStatus(pr)
				if err != nil {
					o.warnf("Failed to query the Pull Request last commit status for %s ref %s %s\n", pr.URL, pr.LastCommitSha, err)
					//return fmt.Errorf("Failed to query the Pull Request last commit status for %s ref %s %s", pr.URL, pr.LastCommitSha, err)
				} else {
					if status == "success" {
						if !o.NoMergePullRequest {
							err = gitProvider.MergePullRequest(pr, "jx promote automatically merged promotion PR")
							if err != nil {
								if !logMergeFailure {
									logMergeFailure = true
									o.warnf("Failed to merge the Pull Request %s due to %s maybe I don't have karma?\n", pr.URL, err)
								}
							}
						}
					} else if status == "error" || status == "failure" {
						return fmt.Errorf("Pull request %s last commit has status %s for ref %s", pr.URL, status, pr.LastCommitSha)
					}
				}
			}
			if time.Now().After(end) {
				return fmt.Errorf("Timed out waiting for pull request %s to merge. Waited %s", pr.URL, duration.String())
			}
			time.Sleep(*o.PullRequestPollDuration)
		}
	}
	return nil
}
