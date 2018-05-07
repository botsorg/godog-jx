package cmd

import (
	"fmt"
	"io"

	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/spf13/cobra"
)

var (
	createChatServer_long = templates.LongDesc(`
		Adds a new Chat Server URL
`)

	createChatServer_example = templates.Examples(`
		# Add a new chat server URL
		jx create chat server slack https://myroom.slack.server
	`)
)

// CreateChatServerOptions the options for the create spring command
type CreateChatServerOptions struct {
	CreateOptions

	Name string
}

// NewCmdCreateChatServer creates a command object for the "create" command
func NewCmdCreateChatServer(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &CreateChatServerOptions{
		CreateOptions: CreateOptions{
			CommonOptions: CommonOptions{
				Factory: f,
				Out:     out,
				Err:     errOut,
			},
		},
	}

	cmd := &cobra.Command{
		Use:     "server kind [url]",
		Short:   "Creates a new chat server URL",
		Aliases: []string{"provider"},
		Long:    createChatServer_long,
		Example: createChatServer_example,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}

	cmd.Flags().StringVarP(&options.Name, "name", "n", "", "The name for the chat server being created")
	return cmd
}

// Run implements the command
func (o *CreateChatServerOptions) Run() error {
	args := o.Args
	if len(args) < 1 {
		return missingChatArguments()
	}
	kind := args[0]
	name := o.Name
	if name == "" {
		name = kind
	}
	gitUrl := ""
	if len(args) > 1 {
		gitUrl = args[1]
	}

	if gitUrl == "" {
		return missingChatArguments()
	}
	authConfigSvc, err := o.CreateChatAuthConfigService()
	if err != nil {
		return err
	}
	config := authConfigSvc.Config()
	config.GetOrCreateServerName(gitUrl, name, kind)
	config.CurrentServer = gitUrl
	err = authConfigSvc.SaveConfig()
	if err != nil {
		return err
	}
	o.Printf("Added issue chat server %s for URL %s\n", util.ColorInfo(name), util.ColorInfo(gitUrl))
	return nil
}

func missingChatArguments() error {
	return fmt.Errorf("Missing chat server URL arguments. Usage: jx create chat server kind [url]")
}
