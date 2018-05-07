package cmd

import (
	"fmt"
	"io"
	"os/user"
	"strings"

	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	"github.com/spf13/cobra"

	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/jenkins-x/jx/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	deleteDevPodLong = templates.LongDesc(`
		Deletes one or more DevPods

		For more documentation see: [http://jenkins-x.io/developing/devpods/](http://jenkins-x.io/developing/devpods/)

`)

	deleteDevPodExample = templates.Examples(`
		# deletes a DevPod by picking one from the list and confirming to it
		jx delete devpod

		# delete a specific DevPod
		jx delete devpod myuser-maven2
	`)
)

// DeleteDevPodOptions are the flags for delete commands
type DeleteDevPodOptions struct {
	CommonOptions
}

// NewCmdDeleteDevPod creates a command object for the generic "get" action, which
// retrieves one or more resources from a server.
func NewCmdDeleteDevPod(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &DeleteDevPodOptions{
		CommonOptions: CommonOptions{
			Factory: f,
			Out:     out,
			Err:     errOut,
		},
	}

	cmd := &cobra.Command{
		Use:     "devpod",
		Short:   "Deletes one or many DevPods",
		Long:    deleteDevPodLong,
		Example: deleteDevPodExample,
		Aliases: []string{"buildpod", "buildpods", "devpods"},
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}

	return cmd
}

// Run implements this command
func (o *DeleteDevPodOptions) Run() error {
	args := o.Args

	client, curNs, err := o.KubeClient()
	if err != nil {
		return err
	}
	ns, _, err := kube.GetDevNamespace(client, curNs)
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}

	username := u.Username
	names, err := kube.GetPodNames(client, ns, username)
	if err != nil {
		return err
	}

	info := util.ColorInfo
	if len(names) == 0 {
		return fmt.Errorf("There are no DevPods for user %s in namespace %s. You can create one via: %s\n", info(username), info(ns), info("jx create devpod"))
	}

	if len(args) == 0 {
		args, err = util.PickNames(names, "Pick DevPod:")
		if err != nil {
			return err
		}
	}
	deletePods := strings.Join(args, ", ")

	if !util.Confirm("You are about to delete the DevPods: "+deletePods, false, "The list of DevPods names to be deleted") {
		return nil
	}
	for _, name := range args {
		if util.StringArrayIndex(names, name) < 0 {
			return util.InvalidOption(optionLabel, name, names)
		}
		err = client.CoreV1().Pods(ns).Delete(name, &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}
	o.Printf("Deleted DevPods %s\n", util.ColorInfo(deletePods))
	return nil
}
