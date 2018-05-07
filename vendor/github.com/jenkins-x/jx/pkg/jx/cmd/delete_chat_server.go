package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/spf13/cobra"
)

var (
	deleteChatServer_long = templates.LongDesc(`
		Deletes one or more chat servers from your local settings
`)

	deleteChatServer_example = templates.Examples(`
		# Deletes an chat server
		jx delete chat server MyProvider
	`)
)

// DeleteChatServerOptions the options for the create spring command
type DeleteChatServerOptions struct {
	CommonOptions

	IgnoreMissingServer bool
}

// NewCmdDeleteChatServer defines the command
func NewCmdDeleteChatServer(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &DeleteChatServerOptions{
		CommonOptions: CommonOptions{
			Factory: f,
			Out:     out,
			Err:     errOut,
		},
	}

	cmd := &cobra.Command{
		Use:     "server",
		Short:   "Deletes one or more chat server(s)",
		Long:    deleteChatServer_long,
		Example: deleteChatServer_example,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}
	cmd.Flags().BoolVarP(&options.IgnoreMissingServer, "ignore-missing", "i", false, "Silently ignore attempts to remove an chat server name that does not exist")
	return cmd
}

// Run implements the command
func (o *DeleteChatServerOptions) Run() error {
	args := o.Args
	if len(args) == 0 {
		return fmt.Errorf("Missing chat server name argument")
	}
	authConfigSvc, err := o.CreateChatAuthConfigService()
	if err != nil {
		return err
	}
	config := authConfigSvc.Config()

	serverNames := config.GetServerNames()
	for _, arg := range args {
		idx := config.IndexOfServerName(arg)
		if idx < 0 {
			if o.IgnoreMissingServer {
				return nil
			}
			return util.InvalidArg(arg, serverNames)
		}
		config.Servers = append(config.Servers[0:idx], config.Servers[idx+1:]...)
	}
	err = authConfigSvc.SaveConfig()
	if err != nil {
		return err
	}
	o.Printf("Deleted chat servers: %s from local settings\n", util.ColorInfo(strings.Join(args, ", ")))
	return nil
}
