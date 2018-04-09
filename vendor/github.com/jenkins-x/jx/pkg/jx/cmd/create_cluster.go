package cmd

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"

	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/spf13/cobra"
	"gopkg.in/AlecAivazis/survey.v1"
)

type KubernetesProvider string

// CreateClusterOptions the flags for running crest cluster
type CreateClusterOptions struct {
	CreateOptions
	InstallOptions InstallOptions
	Flags          InitFlags
	Provider       string
}

const (
	GKE        = "gke"
	EKS        = "eks"
	AKS        = "aks"
	AWS        = "aws"
	MINIKUBE   = "minikube"
	MINISHIFT  = "minishift"
	KUBERNETES = "kubernetes"
	OPENSHIFT  = "openshift"
	JX_INFRA   = "jx-infra"

	optionKubernetesVersion = "kubernetes-version"
	optionNodes             = "nodes"
	optionClusterName       = "cluster-name"
)

var KUBERNETES_PROVIDERS = []string{MINIKUBE, GKE, AKS, AWS, EKS, KUBERNETES, OPENSHIFT, MINISHIFT, JX_INFRA}

const (
	stableKubeCtlVersionURL = "https://storage.googleapis.com/kubernetes-release/release/stable.txt"

	valid_providers = `Valid kubernetes providers include:

    * aks (Azure Container Service - https://docs.microsoft.com/en-us/azure/aks)
    * aws (Amazon Web Services via kops - https://github.com/aws-samples/aws-workshop-for-kubernetes/blob/master/readme.adoc)
    * gke (Google Container Engine - https://cloud.google.com/kubernetes-engine)
    * kubernetes for custom installations of Kubernetes
    * minikube (single-node Kubernetes cluster inside a VM on your laptop)
    * coming soon:
        eks (Amazon Elastic Container Service - https://aws.amazon.com/eks)    `
)

type CreateClusterFlags struct {
}

var (
	createClusterLong = templates.LongDesc(`
		This command creates a new kubernetes cluster, installing required local dependencies and provisions the Jenkins X platform

		You can see a demo of this command here: [http://jenkins-x.io/demos/create_cluster/](http://jenkins-x.io/demos/create_cluster/)

		%s

		Depending on which cloud provider your cluster is created on possible dependencies that will be installed are:

		- kubectl (CLI to interact with kubernetes clusters)
		- helm (package manager for kubernetes)
		- draft (CLI that makes it easy to build applications that run on kubernetes)
		- minikube (single-node Kubernetes cluster inside a VM on your laptop )
		- virtualisation drivers (to run minikube in a VM)
		- gcloud (Google Cloud CLI)
		- az (Azure CLI)

		For more documentation see: [http://jenkins-x.io/getting-started/create-cluster/](http://jenkins-x.io/getting-started/create-cluster/)

`)

	createClusterExample = templates.Examples(`

		jx create cluster minikube

`)
)

// KubernetesProviderOptions returns all the kubernetes providers as a string
func KubernetesProviderOptions() string {
	values := []string{}
	values = append(values, KUBERNETES_PROVIDERS...)
	sort.Strings(values)
	return strings.Join(values, ", ")
}

// NewCmdGet creates a command object for the generic "init" action, which
// installs the dependencies required to run the jenkins-x platform on a kubernetes cluster.
func NewCmdCreateCluster(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := createCreateClusterOptions(f, out, errOut, "")

	cmd := &cobra.Command{
		Use:     "cluster [kubernetes provider]",
		Short:   "Create a new kubernetes cluster",
		Long:    fmt.Sprintf(createClusterLong, valid_providers),
		Example: createClusterExample,
		Run: func(cmd2 *cobra.Command, args []string) {
			options.Cmd = cmd2
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}

	cmd.AddCommand(NewCmdCreateClusterAKS(f, out, errOut))
	cmd.AddCommand(NewCmdCreateClusterAWS(f, out, errOut))
	cmd.AddCommand(NewCmdCreateClusterGKE(f, out, errOut))
	cmd.AddCommand(NewCmdCreateClusterMinikube(f, out, errOut))

	return cmd
}

func createCreateClusterOptions(f cmdutil.Factory, out io.Writer, errOut io.Writer, cloudProvider string) CreateClusterOptions {
	commonOptions := CommonOptions{
		Factory: f,
		Out:     out,
		Err:     errOut,
	}
	options := CreateClusterOptions{
		CreateOptions: CreateOptions{
			CommonOptions: commonOptions,
		},
		Provider:       cloudProvider,
		InstallOptions: createInstallOptions(f, out, errOut),
	}
	return options
}

func (o *CreateClusterOptions) initAndInstall(provider string) error {
	// call jx init
	o.InstallOptions.BatchMode = o.BatchMode
	o.InstallOptions.Flags.Provider = provider

	// call jx install
	installOpts := &o.InstallOptions

	// lets default the helm domain
	exposeController := o.InstallOptions.CreateEnvOptions.HelmValuesConfig.ExposeController
	if exposeController != nil && exposeController.Config.Domain == "" && installOpts.Flags.Domain != "" {
		exposeController.Config.Domain = installOpts.Flags.Domain
	}

	err := installOpts.Run()
	if err != nil {
		return err
	}

	return nil
}

func (o *CreateClusterOptions) Run() error {
	return o.Cmd.Help()
}

func (o *CreateClusterOptions) addCreateClusterFlags(cmd *cobra.Command) {

	o.InstallOptions.addInstallFlags(cmd, true)
}

func (o *CreateClusterOptions) getClusterDependencies(deps []string) []string {
	d := binaryShouldBeInstalled("kubectl")
	if d != "" {
		deps = append(deps, d)
	}

	d = binaryShouldBeInstalled("helm")
	if d != "" {
		deps = append(deps, d)
	}

	// Platform specific deps
	if runtime.GOOS == "darwin" {
		if !o.NoBrew {
			d = binaryShouldBeInstalled("brew")
			if d != "" {
				deps = append(deps, d)
			}
		}
	}
	return deps
}

func (o *CreateClusterOptions) installMissingDependencies(providerSpecificDeps []string) error {

	// get base list of required dependencies and add provider specific ones
	deps := o.getClusterDependencies(providerSpecificDeps)

	if len(deps) == 0 {
		return nil
	}

	if o.BatchMode {
		return errors.New(fmt.Sprintf("run without batch mode or mannually install missing dependencies %v\n", deps))
	}
	install := []string{}
	prompt := &survey.MultiSelect{
		Message: "Missing required dependencies, deselect to avoid auto installing:",
		Options: deps,
		Default: deps,
	}
	survey.AskOne(prompt, &install, nil)

	return o.doInstallMissingDependencies(install)
}
