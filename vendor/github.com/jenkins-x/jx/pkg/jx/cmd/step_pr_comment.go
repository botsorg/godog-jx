package cmd

import (
	"io"

	"github.com/spf13/cobra"

	"strconv"

	"fmt"

	"github.com/jenkins-x/jx/pkg/gits"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
)

// GetOptions is the start of the data required to perform the operation.  As new fields are added, add them here instead of
// referencing the cmd.Flags()
type StepPRCommentOptions struct {
	StepPROptions
	Flags StepPRCommentFlags
}

type StepPRCommentFlags struct {
	Comment    string
	URL        string
	Owner      string
	Repository string
	PR         string
}

// NewCmdStep Steps a command object for the "step" command
func NewCmdStepPRComment(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &StepPRCommentOptions{
		StepPROptions: StepPROptions{
			StepOptions: StepOptions{
				CommonOptions: CommonOptions{
					Factory: f,
					Out:     out,
					Err:     errOut,
				},
			},
		},
	}

	cmd := &cobra.Command{
		Use:   "comment",
		Short: "pipeline step pr comment",
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}

	cmd.Flags().StringVarP(&options.Flags.Comment, "comment", "c", "", "comment to add to the Pull Request")
	cmd.Flags().StringVarP(&options.Flags.Owner, "owner", "o", "", "git organisation / owner")
	cmd.Flags().StringVarP(&options.Flags.Repository, "repository", "r", "", "git repository")
	cmd.Flags().StringVarP(&options.Flags.PR, "pull-request", "p", "", "git pull request number")

	options.addCommonFlags(cmd)

	return cmd
}

// Run implements this command
func (o *StepPRCommentOptions) Run() error {
	if o.Flags.PR == "" {
		return fmt.Errorf("no pull request number provided")
	}
	if o.Flags.Owner == "" {
		return fmt.Errorf("no git owner provided")
	}
	if o.Flags.Repository == "" {
		return fmt.Errorf("no git repository provided")
	}
	if o.Flags.Comment == "" {
		return fmt.Errorf("no comment provided")
	}

	authConfigSvc, err := o.Factory.CreateGitAuthConfigService()
	if err != nil {
		return err
	}

	gitInfo, err := gits.GetGitInfo("")
	if err != nil {
		return err
	}
	gitKind, err := o.GitServerKind(gitInfo)
	if err != nil {
		return err
	}

	provider, err := gitInfo.PickOrCreateProvider(authConfigSvc, "user name to submit comment as", o.BatchMode, gitKind)
	if err != nil {
		return err
	}

	prNumber, err := strconv.Atoi(o.Flags.PR)
	if err != nil {
		return err
	}

	pr := gits.GitPullRequest{
		Repo:   o.Flags.Repository,
		Owner:  o.Flags.Owner,
		Number: &prNumber,
	}

	return provider.AddPRComment(&pr, o.Flags.Comment)
}
