package cmd

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ghodss/yaml"
	"github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	optionLabel      = "label"
	optionRequestCpu = "request-cpu"
	devPodGoPath     = "/home/jenkins/go"
)

var (
	createDevPodLong = templates.LongDesc(`
		Creates a new DevPod

		For more documentation see: [http://jenkins-x.io/developing/devpods/](http://jenkins-x.io/developing/devpods/)

`)

	createDevPodExample = templates.Examples(`
		# creates a new DevPod asking the user for the label to use
		jx create devpod

		# creates a new Maven DevPod 
		jx create devpod -l maven
	`)
)

// CreateDevPodOptions the options for the create spring command
type CreateDevPodOptions struct {
	CreateOptions

	Label      string
	Suffix     string
	WorkingDir string
	RequestCpu string
}

// NewCmdCreateDevPod creates a command object for the "create" command
func NewCmdCreateDevPod(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &CreateDevPodOptions{
		CreateOptions: CreateOptions{
			CommonOptions: CommonOptions{
				Factory: f,
				Out:     out,
				Err:     errOut,
			},
		},
	}

	cmd := &cobra.Command{
		Use:     "devpod",
		Short:   "Creates a Developer Pod for running builds and tests inside the cluster",
		Aliases: []string{"dpod", "buildpod"},
		Long:    createDevPodLong,
		Example: createDevPodExample,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}

	cmd.Flags().StringVarP(&options.Label, optionLabel, "l", "", "The label of the pod template to use")
	cmd.Flags().StringVarP(&options.Suffix, "suffix", "s", "", "The suffix to append the pod name")
	cmd.Flags().StringVarP(&options.WorkingDir, "working-dir", "w", "", "The working directory of the dev pod")
	cmd.Flags().StringVarP(&options.RequestCpu, optionRequestCpu, "c", "1.4", "The request CPU of the dev pod")
	options.addCommonFlags(cmd)
	return cmd
}

// Run implements this command
func (o *CreateDevPodOptions) Run() error {
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
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	cm, err := client.CoreV1().ConfigMaps(ns).Get(kube.ConfigMapJenkinsPodTemplates, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Failed to find ConfigMap %s in namespace %s: %s", kube.ConfigMapJenkinsPodTemplates, ns, err)
	}
	podTemplates := cm.Data
	labels := util.SortedMapKeys(podTemplates)

	label := o.Label
	if label == "" {
		label = o.guessDevPodLabel(dir)
	}
	if label == "" {
		label, err = util.PickName(labels, "Pick which kind of dev pod you wish to create: ")
		if err != nil {
			return err
		}
	}
	yml := podTemplates[label]
	if yml == "" {
		return util.InvalidOption(optionLabel, label, labels)
	}

	o.Printf("Creating a dev pod of label: %s\n", label)

	editEnv, err := o.getOrCreateEditEnvironment()
	if err != nil {
		return err
	}

	pod := &corev1.Pod{}
	err = yaml.Unmarshal([]byte(yml), pod)
	if err != nil {
		return fmt.Errorf("Failed to parse Pod Template YAML: %s\n%s", err, yml)
	}
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	// lets remove the workspace volume as that breaks kync
	workspaceVolume := "workspace-volume"
	for i, v := range pod.Spec.Volumes {
		if v.Name == workspaceVolume {
			pod.Spec.Volumes = append(pod.Spec.Volumes[:i], pod.Spec.Volumes[i+1:]...)
			break
		}
	}
	for ci, c := range pod.Spec.Containers {
		for i, v := range c.VolumeMounts {
			if v.Name == workspaceVolume {
				pod.Spec.Containers[ci].VolumeMounts = append(c.VolumeMounts[:i], c.VolumeMounts[i+1:]...)
				break
			}
		}
	}

	userName := u.Username
	name := kube.ToValidName(userName + "-" + label)
	if o.Suffix != "" {
		name += "-" + o.Suffix
	}
	names, err := kube.GetPodNames(client, ns, "")
	if err != nil {
		return err
	}

	name = uniquePodName(names, name)

	pod.Name = name
	pod.Labels[kube.LabelPodTemplate] = label
	pod.Labels[kube.LabelDevPodName] = name
	pod.Labels[kube.LabelDevPodUsername] = userName

	if len(pod.Spec.Containers) == 0 {
		return fmt.Errorf("No containers specified for label %s with YAML: %s", label, yml)
	}
	container1 := &pod.Spec.Containers[0]

	if o.RequestCpu != "" {
		q, err := resource.ParseQuantity(o.RequestCpu)
		if err != nil {
			return util.InvalidOptionError(optionRequestCpu, o.RequestCpu, err)
		}
		container1.Resources.Requests[corev1.ResourceCPU] = q
	}

	workingDir := o.WorkingDir
	if workingDir == "" {
		workingDir = "/code"

		// lets check for gopath stuff
		gopath := os.Getenv("GOPATH")
		if gopath != "" {
			rel, err := filepath.Rel(gopath, dir)
			if err == nil && rel != "" {
				workingDir = filepath.Join(devPodGoPath, rel)
			}
		}
	}
	pod.Annotations[kube.AnnotationWorkingDir] = workingDir
	container1.Env = append(container1.Env, corev1.EnvVar{
		Name:  "WORK_DIR",
		Value: workingDir,
	})
	container1.Stdin = true

	if editEnv != nil {
		container1.Env = append(container1.Env, corev1.EnvVar{
			Name:  "SKAFFOLD_DEPLOY_NAMESPACE",
			Value: editEnv.Spec.Namespace,
		})
	}

	_, err = client.CoreV1().Pods(ns).Create(pod)
	if err != nil {
		if o.Verbose {
			return fmt.Errorf("Failed to create pod %s\nYAML: %s", err, yml)
		} else {
			return fmt.Errorf("Failed to create pod %s", err)
		}
	}

	o.Printf("Created pod %s - waiting for it to be ready...\n", util.ColorInfo(name))
	err = kube.WaitForPodNameToBeReady(client, ns, name, time.Hour)
	if err != nil {
		return err
	}

	o.Printf("Pod %s is now ready!\n", util.ColorInfo(name))
	o.Printf("You can open other shells into this DevPod via %s\n", util.ColorInfo("jx rsh -d"))

	options := &RshOptions{
		CommonOptions: o.CommonOptions,
		Namespace:     ns,
		Pod:           name,
		DevPod:        true,
	}
	options.Args = []string{}
	return options.Run()
}

func (o *CreateDevPodOptions) getOrCreateEditEnvironment() (*v1.Environment, error) {
	var env *v1.Environment
	apisClient, err := o.Factory.CreateApiExtensionsClient()
	if err != nil {
		return env, err
	}
	err = kube.RegisterEnvironmentCRD(apisClient)
	if err != nil {
		return env, err
	}

	kubeClient, _, err := o.KubeClient()
	if err != nil {
		return env, err
	}

	jxClient, ns, err := o.JXClientAndDevNamespace()
	if err != nil {
		return env, err
	}
	u, err := user.Current()
	if err != nil {
		return env, err
	}
	env, err = kube.EnsureEditEnvironmentSetup(kubeClient, jxClient, ns, u.Username)
	if err != nil {
		return env, err
	}
	// lets ensure that we've installed the exposecontroller service in the namespace
	var flag bool
	editNs := env.Spec.Namespace
	flag, err = kube.IsDeploymentRunning(kubeClient, kube.DeploymentExposecontrollerService, editNs)
	if !flag || err != nil {
		o.Printf("Installing the ExposecontrollerService in the namespace: %s\n", util.ColorInfo(editNs))
		releaseName := editNs + "-es"
		err = o.installChart(releaseName, kube.ChartExposecontrollerService, "", editNs, true, nil)
	}
	return env, err
}

func (o *CreateDevPodOptions) guessDevPodLabel(dir string) string {
	gopath := os.Getenv("GOPATH")
	if gopath != "" {
		rel, err := filepath.Rel(gopath, dir)
		if err == nil && rel != "" {
			return "go"
		}
	}
	return ""
}

func uniquePodName(names []string, prefix string) string {
	count := 1
	for {
		name := prefix
		if count > 1 {
			name += strconv.Itoa(count)
		}
		if util.StringArrayIndex(names, name) < 0 {
			return name
		}
		count++
	}
}
