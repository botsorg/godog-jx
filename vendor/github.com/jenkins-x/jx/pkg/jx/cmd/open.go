package cmd

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
)

type OpenOptions struct {
	ConsoleOptions
}

var (
	open_long = templates.LongDesc(`
		Opens a named service in the browser.

		You can use the '--url' argument to just display the URL without opening it`)

	open_example = templates.Examples(`
		# Open the Nexus console in a browser
		jx open jenkins-x-sonatype-nexus

		# Print the Nexus console URL but do not open a browser
		jx open jenkins-x-sonatype-nexus -u

		# List all the service URLs
		jx open`)
)

func NewCmdOpen(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &OpenOptions{
		ConsoleOptions{
			CommonOptions: CommonOptions{
				Factory: f,
				Out:     out,
				Err:     errOut,
			},
		},
	}
	cmd := &cobra.Command{
		Use:     "open",
		Short:   "Open a service in a browser",
		Long:    open_long,
		Example: open_example,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}
	cmd.Flags().BoolVarP(&options.OnlyViewURL, "url", "u", false, "Only displays and the URL and does not open the browser")
	return cmd
}

func (o *OpenOptions) Run() error {
	if len(o.Args) == 0 {
		getOptions := GetURLOptions{
			GetOptions: GetOptions{
				CommonOptions: o.CommonOptions,
			},
		}
		return getOptions.Run()
	}
	name := o.Args[0]
	return o.ConsoleOptions.Open(name, name)
}
