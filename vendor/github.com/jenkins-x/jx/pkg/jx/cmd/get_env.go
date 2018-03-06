package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetEnvOptions containers the CLI options
type GetEnvOptions struct {
	GetOptions

	PromotionStrategy string
}

var (
	get_env_long = templates.LongDesc(`
		Display one or many environments.
`)

	get_env_example = templates.Examples(`
		# List all environments
		jx get environments

		# List all environments using the shorter alias
		jx get env
	`)
)

// NewCmdGetEnv creates the new command for: jx get env
func NewCmdGetEnv(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &GetEnvOptions{
		GetOptions: GetOptions{
			CommonOptions: CommonOptions{
				Factory: f,
				Out:     out,
				Err:     errOut,
			},
		},
	}
	cmd := &cobra.Command{
		Use:     "environments",
		Short:   "Display one or many Environments",
		Aliases: []string{"envs", "environment", "env"},
		Long:    get_env_long,
		Example: get_env_example,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}

	options.addGetFlags(cmd)

	cmd.Flags().StringVarP(&options.PromotionStrategy, "promote", "p", "", "Filters the environments by promotion strategy. Possible values: "+strings.Join(v1.PromotionStrategyTypeValues, ", "))

	return cmd
}

// Run implements this command
func (o *GetEnvOptions) Run() error {
	f := o.Factory
	client, currentNs, err := f.CreateJXClient()
	if err != nil {
		return err
	}
	kubeClient, _, err := o.Factory.CreateClient()
	if err != nil {
		return err
	}
	ns, _, err := kube.GetDevNamespace(kubeClient, currentNs)
	if err != nil {
		return err
	}
	apisClient, err := f.CreateApiExtensionsClient()
	if err != nil {
		return err
	}
	err = kube.RegisterEnvironmentCRD(apisClient)
	if err != nil {
		return err
	}

	args := o.Args
	if len(args) > 0 {
		e := args[0]
		env, err := client.JenkinsV1().Environments(ns).Get(e, metav1.GetOptions{})
		if err != nil {
			envNames, err := kube.GetEnvironmentNames(client, ns)
			if err != nil {
				return err
			}
			return util.InvalidArg(e, envNames)
		}

		// lets output one environment
		spec := &env.Spec

		table := o.CreateTable()
		table.AddRow("NAME", "LABEL", "KIND", "NAMESPACE", "SOURCE", "REF", "PR")
		table.AddRow(e, spec.Label, spec.Namespace, kindString(spec), spec.Source.URL, spec.Source.Ref, spec.PullRequestURL)
		table.Render()
		o.Printf("\n")

		ens := env.Spec.Namespace
		if ens != "" {
			deps, err := kubeClient.AppsV1beta1().Deployments(ens).List(metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("Could not find deployments in namespace %s: %s", ens, err)
			}
			table = o.CreateTable()
			table.AddRow("APP", "VERSION", "DESIRED", "CURRENT", "UP-TO-DATE", "AVAILABLE", "AGE")
			for _, d := range deps.Items {
				replicas := ""
				if d.Spec.Replicas != nil {
					replicas = formatInt32(*d.Spec.Replicas)
				}
				table.AddRow(d.Name, kube.GetVersion(&d.ObjectMeta), replicas,
					formatInt32(d.Status.ReadyReplicas), formatInt32(d.Status.UpdatedReplicas), formatInt32(d.Status.AvailableReplicas), "")
			}
			table.Render()
		}
	} else {
		envs, err := client.JenkinsV1().Environments(ns).List(metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(envs.Items) == 0 {
			o.Printf("No environments found.\nTo create an environment use: jx create env\n")
			return nil
		}

		environments := o.filterEnvironments(envs.Items)
		kube.SortEnvironments(environments)

		if o.Output != "" {
			envs.Items = environments
			return o.renderResult(envs, o.Output)
		}
		table := o.CreateTable()
		table.AddRow("NAME", "LABEL", "KIND", "PROMOTE", "NAMESPACE", "ORDER", "CLUSTER", "SOURCE", "REF", "PR")

		for _, env := range environments {
			spec := &env.Spec
			table.AddRow(env.Name, spec.Label, kindString(spec), string(spec.PromotionStrategy), spec.Namespace, util.Int32ToA(spec.Order), spec.Cluster, spec.Source.URL, spec.Source.Ref, spec.PullRequestURL)
		}
		table.Render()
	}
	return nil
}

func kindString(spec *v1.EnvironmentSpec) string {
	answer := string(spec.Kind)
	if answer == "" {
		return string(v1.EnvironmentKindTypePermanent)
	}
	return answer
}

func (o *GetEnvOptions) filterEnvironments(envs []v1.Environment) []v1.Environment {
	answer := []v1.Environment{}
	for _, e := range envs {
		if o.matchesFilter(&e) {
			answer = append(answer, e)
		}
	}
	return answer
}

func (o *GetEnvOptions) matchesFilter(env *v1.Environment) bool {
	if o.PromotionStrategy == "" {
		return true
	}
	return env.Spec.PromotionStrategy == v1.PromotionStrategyType(o.PromotionStrategy)
}
