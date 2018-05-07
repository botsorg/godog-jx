package cmd

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/blang/semver"
	"github.com/jenkins-x/jx/pkg/jx/cmd/log"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/pborman/uuid"
	"gopkg.in/AlecAivazis/survey.v1"
)

func (o *CommonOptions) doInstallMissingDependencies(install []string) error {
	// install package managers first
	for _, i := range install {
		if i == "brew" {
			o.installBrew()
			break
		}
	}

	for _, i := range install {
		var err error
		switch i {
		case "az":
			err = o.installAzureCli()
		case "kubectl":
			err = o.installKubectl()
		case "gcloud":
			err = o.installGcloud()
		case "helm":
			err = o.installHelm()
		case "hyperkit":
			err = o.installHyperkit()
		case "kops":
			err = o.installKops()
		case "kvm":
			err = o.installKvm()
		case "kvm2":
			err = o.installKvm2()
		case "ksync":
			_, err = o.installKSync()
		case "minikube":
			err = o.installMinikube()
		case "minishift":
			err = o.installMinishift()
		case "oc":
			err = o.installOc()
		case "virtualbox":
			err = o.installVirtualBox()
		case "xhyve":
			err = o.installXhyve()
		default:
			return fmt.Errorf("unknown dependency to install %s\n", i)
		}
		if err != nil {
			return fmt.Errorf("error installing %s: %v\n", i, err)
		}
	}
	return nil
}

// appends the binary to the deps array if it cannot be found on the $PATH
func binaryShouldBeInstalled(d string) string {
	_, err := exec.LookPath(d)
	if err != nil {
		// look for windows exec
		if runtime.GOOS == "windows" {
			d2 := d + ".exe"
			_, err = exec.LookPath(d2)
			if err == nil {
				return ""
			}
		}
		log.Infof("%s not found\n", d)
		return d
	}
	return ""
}

func (o *CommonOptions) installBrew() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return o.runCommand("/usr/bin/ruby", "-e", "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)")
}

func (o *CommonOptions) shouldInstallBinary(binDir string, name string) (fileName string, download bool, err error) {
	fileName = name
	download = false
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	pgmPath, err := exec.LookPath(fileName)
	if err == nil {
		o.Printf("%s is already available on your PATH at %s\n", util.ColorInfo(fileName), util.ColorInfo(pgmPath))
		return
	}

	// lets see if its been installed but just is not on the PATH
	exists, err := util.FileExists(filepath.Join(binDir, fileName))
	if err != nil {
		return
	}
	if exists {
		o.warnf("Please add %s to your PATH\n", util.ColorInfo(binDir))
		return
	}
	download = true
	return
}

func (o *CommonOptions) downloadFile(clientURL string, fullPath string) error {
	o.Printf("Downloading %s to %s...\n", util.ColorInfo(clientURL), util.ColorInfo(fullPath))
	err := util.DownloadFile(fullPath, clientURL)
	if err != nil {
		return fmt.Errorf("Unable to download file %s from %s due to: %v", fullPath, clientURL, err)
	}
	fmt.Printf("Downloaded %s\n", util.ColorInfo(fullPath))
	return nil
}

func (o *CommonOptions) installBrewIfRequired() error {
	if runtime.GOOS != "darwin" || o.NoBrew {
		return nil
	}

	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	_, flag, err := o.shouldInstallBinary(binDir, "brew")
	if err != nil || !flag {
		return err
	}
	return o.installBrew()
}

func (o *CommonOptions) installKubectl() error {
	if runtime.GOOS == "darwin" && !o.NoBrew {
		return o.runCommand("brew", "install", "kubectl")
	}
	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	fileName, flag, err := o.shouldInstallBinary(binDir, "kubectl")
	if err != nil || !flag {
		return err
	}
	kubernetes := "kubernetes"
	latestVersion, err := o.getLatestVersionFromKubernetesReleaseUrl()
	if err != nil {
		return fmt.Errorf("Unable to get latest version for github.com/%s/%s %v", kubernetes, kubernetes, err)
	}

	clientURL := fmt.Sprintf("https://storage.googleapis.com/kubernetes-release/release/v%s/bin/%s/%s/%s", latestVersion, runtime.GOOS, runtime.GOARCH, fileName)
	fullPath := filepath.Join(binDir, fileName)
	tmpFile := fullPath + ".tmp"
	err = o.downloadFile(clientURL, tmpFile)
	if err != nil {
		return err
	}
	err = util.RenameFile(tmpFile, fullPath)
	if err != nil {
		return err
	}
	return os.Chmod(fullPath, 0755)
}

func (o *CommonOptions) installOc() error {
	// need to fix the version we download as not able to work out the oc sha in the URL yet
	sha := "191fece"
	latestVersion := "3.9.0"

	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	binary := "oc"
	fileName, flag, err := o.shouldInstallBinary(binDir, binary)
	if err != nil || !flag {
		return err
	}

	var arch string
	clientURL := fmt.Sprintf("https://github.com/openshift/origin/releases/download/v%s/openshift-origin-client-tools-v%s-%s", latestVersion, latestVersion, sha)

	extension := ".zip"
	switch runtime.GOOS {
	case "windows":
		clientURL += "-windows.zip"
	case "darwin":
		clientURL += "-mac.zip"
	default:
		switch runtime.GOARCH {
		case "amd64":
			arch = "64bit"
		case "386":
			arch = "32bit"
		}
		extension = ".tar.gz"
		clientURL += fmt.Sprintf("-%s-%s.tar.gz", runtime.GOOS, arch)
	}

	fullPath := filepath.Join(binDir, fileName)
	tarFile := filepath.Join(binDir, "oc.tgz")
	if extension == ".zip" {
		tarFile = filepath.Join(binDir, "oc.zip")
	}
	err = o.downloadFile(clientURL, tarFile)
	if err != nil {
		return err
	}

	if extension == ".zip" {
		zipDir := filepath.Join(binDir, "oc-tmp-"+uuid.NewUUID().String())
		err = os.MkdirAll(zipDir, DefaultWritePermissions)
		if err != nil {
			return err
		}
		err = util.Unzip(tarFile, zipDir)
		if err != nil {
			return err
		}
		f := filepath.Join(zipDir, fileName)
		exists, err := util.FileExists(f)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("Could not find file %s inside the downloaded oc.zip!", f)
		}
		err = os.Rename(f, fullPath)
		if err != nil {
			return err
		}
		err = os.RemoveAll(zipDir)
	} else {
		err = util.UnTargz(tarFile, binDir, []string{binary, fileName})
	}
	if err != nil {
		return err
	}
	err = os.Remove(tarFile)
	if err != nil {
		return err
	}
	return os.Chmod(fullPath, 0755)
}

// get the latest version from kubernetes, parse it and return it
func (o *CommonOptions) getLatestVersionFromKubernetesReleaseUrl() (sem semver.Version, err error) {
	response, err := http.Get(stableKubeCtlVersionURL)
	if err != nil {
		return semver.Version{}, fmt.Errorf("Cannot get url " + stableKubeCtlVersionURL)
	}
	defer response.Body.Close()

	bytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return semver.Version{}, fmt.Errorf("Cannot get url body")
	}

	s := strings.TrimSpace(string(bytes))
	if s != "" {
		return semver.Make(strings.TrimPrefix(s, "v"))
	}

	return semver.Version{}, fmt.Errorf("Cannot get release name")
}

func (o *CommonOptions) installHyperkit() error {
	/*
		info, err := o.getCommandOutput("", "docker-machine-driver-hyperkit")
		if strings.Contains(info, "Docker") {
			o.Printf("docker-machine-driver-hyperkit is already installed\n")
			return nil
		}
		o.Printf("Result: %s and %v\n", info, err)
		err = o.runCommand("curl", "-LO", "https://storage.googleapis.com/minikube/releases/latest/docker-machine-driver-hyperkit")
		if err != nil {
			return err
		}

		err = o.runCommand("chmod", "+x", "docker-machine-driver-hyperkit")
		if err != nil {
			return err
		}

		log.Warn("Installing hyperkit does require sudo to perform some actions, for more details see https://github.com/kubernetes/minikube/blob/master/docs/drivers.md#hyperkit-driver")

		err = o.runCommand("sudo", "mv", "docker-machine-driver-hyperkit", "/usr/local/bin/")
		if err != nil {
			return err
		}

		err = o.runCommand("sudo", "chown", "root:wheel", "/usr/local/bin/docker-machine-driver-hyperkit")
		if err != nil {
			return err
		}

		return o.runCommand("sudo", "chmod", "u+s", "/usr/local/bin/docker-machine-driver-hyperkit")
	*/
	return nil
}

func (o *CommonOptions) installKvm() error {
	o.warnf("We cannot yet automate the installation of KVM - can you install this manually please?\nPlease see: https://www.linux-kvm.org/page/Downloads\n")
	return nil
}

func (o *CommonOptions) installKvm2() error {
	o.warnf("We cannot yet automate the installation of KVM with KVM2 driver - can you install this manually please?\nPlease see: https://www.linux-kvm.org/page/Downloads " +
		"and https://github.com/kubernetes/minikube/blob/master/docs/drivers.md#kvm2-driver\n")
	return nil
}

func (o *CommonOptions) installVirtualBox() error {
	o.warnf("We cannot yet automate the installation of VirtualBox - can you install this manually please?\nPlease see: https://www.virtualbox.org/wiki/Downloads\n")
	return nil
}

func (o *CommonOptions) installXhyve() error {
	info, err := o.getCommandOutput("", "brew", "info", "docker-machine-driver-xhyve")

	if err != nil || strings.Contains(info, "Not installed") {
		err = o.runCommand("brew", "install", "docker-machine-driver-xhyve")
		if err != nil {
			return err
		}

		brewPrefix, err := o.getCommandOutput("", "brew", "--prefix")
		if err != nil {
			return err
		}

		file := brewPrefix + "/opt/docker-machine-driver-xhyve/bin/docker-machine-driver-xhyve"
		err = o.runCommand("sudo", "chown", "root:wheel", file)
		if err != nil {
			return err
		}

		err = o.runCommand("sudo", "chmod", "u+s", file)
		if err != nil {
			return err
		}
		o.Printf("xhyve driver installed\n")
	} else {
		pgmPath, _ := exec.LookPath("docker-machine-driver-xhyve")
		o.Printf("xhyve driver is already available on your PATH at %s\n", pgmPath)
	}
	return nil
}

func (o *CommonOptions) installHelm() error {
	if runtime.GOOS == "darwin" && !o.NoBrew {
		return o.runCommand("brew", "install", "kubernetes-helm")
	}

	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	binary := "helm"
	fileName, flag, err := o.shouldInstallBinary(binDir, binary)
	if err != nil || !flag {
		return err
	}
	latestVersion, err := util.GetLatestVersionFromGitHub("kubernetes", "helm")
	if err != nil {
		return err
	}
	clientURL := fmt.Sprintf("https://storage.googleapis.com/kubernetes-helm/helm-v%s-%s-%s.tar.gz", latestVersion, runtime.GOOS, runtime.GOARCH)
	fullPath := filepath.Join(binDir, fileName)
	tarFile := fullPath + ".tgz"
	err = o.downloadFile(clientURL, tarFile)
	if err != nil {
		return err
	}
	err = util.UnTargz(tarFile, binDir, []string{binary, fileName})
	if err != nil {
		return err
	}
	err = os.Remove(tarFile)
	if err != nil {
		return err
	}
	return os.Chmod(fullPath, 0755)
}

func (o *CommonOptions) getLatestJXVersion() (semver.Version, error) {
	return util.GetLatestVersionFromGitHub("jenkins-x", "jx")
}

func (o *CommonOptions) installKops() error {
	if runtime.GOOS == "darwin" && !o.NoBrew {
		return o.runCommand("brew", "install", "kops")
	}
	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	binary := "kops"
	fileName, flag, err := o.shouldInstallBinary(binDir, binary)
	if err != nil || !flag {
		return err
	}
	latestVersion, err := util.GetLatestVersionFromGitHub("kubernetes", "kops")
	if err != nil {
		return err
	}
	clientURL := fmt.Sprintf("https://github.com/kubernetes/kops/releases/download/%s/kops-%s-%s", latestVersion, runtime.GOOS, runtime.GOARCH)
	fullPath := filepath.Join(binDir, fileName)
	tmpFile := fullPath + ".tmp"
	err = o.downloadFile(clientURL, tmpFile)
	if err != nil {
		return err
	}
	err = util.RenameFile(tmpFile, fullPath)
	if err != nil {
		return err
	}
	return os.Chmod(fullPath, 0755)
}

func (o *CommonOptions) installKSync() (bool, error) {
	binDir, err := util.BinaryLocation()
	if err != nil {
		return false, err
	}
	binary := "ksync"
	fileName, flag, err := o.shouldInstallBinary(binDir, binary)
	if err != nil || !flag {
		return false, err
	}
	latestVersion, err := util.GetLatestVersionFromGitHub("vapor-ware", "ksync")
	if err != nil {
		return false, err
	}
	clientURL := fmt.Sprintf("https://github.com/vapor-ware/ksync/releases/download/%s/ksync_%s_%s", latestVersion, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		clientURL += ".exe"
	}
	fullPath := filepath.Join(binDir, fileName)
	tmpFile := fullPath + ".tmp"
	err = o.downloadFile(clientURL, tmpFile)
	if err != nil {
		return false, err
	}
	err = util.RenameFile(tmpFile, fullPath)
	if err != nil {
		return false, err
	}
	return true, os.Chmod(fullPath, 0755)
}

func (o *CommonOptions) installJx(upgrade bool, version string) error {
	if runtime.GOOS == "darwin" && !o.NoBrew {
		if upgrade {
			return o.runCommand("brew", "upgrade", "jx")
		} else {
			return o.runCommand("brew", "install", "jx")
		}
	}
	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	binary := "jx"
	fileName := binary
	if !upgrade {
		f, flag, err := o.shouldInstallBinary(binDir, binary)
		if err != nil || !flag {
			return err
		}
		fileName = f
	}
	org := "jenkins-x"
	repo := "jx"
	latestVersion, err := util.GetLatestVersionFromGitHub(org, repo)
	if err != nil {
		return err
	}
	clientURL := fmt.Sprintf("https://github.com/"+org+"/"+repo+"/releases/download/v%s/"+binary+"-%s-%s.tar.gz", latestVersion, runtime.GOOS, runtime.GOARCH)
	fullPath := filepath.Join(binDir, fileName)
	tarFile := fullPath + ".tgz"
	err = o.downloadFile(clientURL, tarFile)
	if err != nil {
		return err
	}
	err = util.UnTargz(tarFile, binDir, []string{binary, fileName})
	if err != nil {
		return err
	}
	err = os.Remove(tarFile)
	if err != nil {
		return err
	}
	return os.Chmod(fullPath, 0755)
}

func (o *CommonOptions) installMinikube() error {
	if runtime.GOOS == "darwin" && !o.NoBrew {
		return o.runCommand("brew", "cask", "install", "minikube")
	}

	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	fileName, flag, err := o.shouldInstallBinary(binDir, "minikube")
	if err != nil || !flag {
		return err
	}
	latestVersion, err := util.GetLatestVersionFromGitHub("kubernetes", "minikube")
	if err != nil {
		return err
	}
	clientURL := fmt.Sprintf("https://github.com/kubernetes/minikube/releases/download/v%s/minikube-%s-%s", latestVersion, runtime.GOOS, runtime.GOARCH)
	fullPath := filepath.Join(binDir, fileName)
	tmpFile := fullPath + ".tmp"
	err = o.downloadFile(clientURL, tmpFile)
	if err != nil {
		return err
	}
	err = util.RenameFile(tmpFile, fullPath)
	if err != nil {
		return err
	}
	return os.Chmod(fullPath, 0755)
}

func (o *CommonOptions) installMinishift() error {
	if runtime.GOOS == "darwin" && !o.NoBrew {
		return o.runCommand("brew", "cask", "install", "minishift")
	}

	binDir, err := util.BinaryLocation()
	if err != nil {
		return err
	}
	fileName, flag, err := o.shouldInstallBinary(binDir, "minishift")
	if err != nil || !flag {
		return err
	}
	latestVersion, err := util.GetLatestVersionFromGitHub("minishift", "minishift")
	if err != nil {
		return err
	}
	clientURL := fmt.Sprintf("https://github.com/minishift/minishift/releases/download/v%s/minikube-%s-%s", latestVersion, runtime.GOOS, runtime.GOARCH)
	fullPath := filepath.Join(binDir, fileName)
	tmpFile := fullPath + ".tmp"
	err = o.downloadFile(clientURL, tmpFile)
	if err != nil {
		return err
	}
	err = util.RenameFile(tmpFile, fullPath)
	if err != nil {
		return err
	}
	return os.Chmod(fullPath, 0755)
}

func (o *CommonOptions) installGcloud() error {
	if runtime.GOOS != "darwin" || o.NoBrew {
		return errors.New("please install missing gloud sdk - see https://cloud.google.com/sdk/downloads#interactive")
	}
	err := o.runCommand("brew", "tap", "caskroom/cask")
	if err != nil {
		return err
	}

	return o.runCommand("brew", "install", "google-cloud-sdk")
}

func (o *CommonOptions) installAzureCli() error {
	return o.runCommand("brew", "install", "azure-cli")
}

func (o *CommonOptions) GetCloudProvider(p string) (string, error) {
	if p == "" {
		// lets detect minikube
		currentContext, err := o.getCommandOutput("", "kubectl", "config", "current-context")
		if err == nil && currentContext == "minikube" {
			p = MINIKUBE
		}
	}
	if p != "" {
		if !util.Contains(KUBERNETES_PROVIDERS, p) {
			return "", util.InvalidArg(p, KUBERNETES_PROVIDERS)
		}
	}

	if p == "" {
		prompt := &survey.Select{
			Message: "Cloud Provider",
			Options: KUBERNETES_PROVIDERS,
			Default: MINIKUBE,
			Help:    "Cloud service providing the kubernetes cluster, local VM (minikube), Google (GKE), Azure (AKS)",
		}

		survey.AskOne(prompt, &p, nil)
	}
	return p, nil
}

func (o *CommonOptions) getClusterDependencies(deps []string) []string {
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

func (o *CommonOptions) installMissingDependencies(providerSpecificDeps []string) error {
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

// installRequirements installs any requirements for the given provider kind
func (o *CommonOptions) installRequirements(cloudProvider string) error {
	var deps []string
	switch cloudProvider {
	case AWS:
		deps = o.addRequiredBinary("kops", deps)
	case AKS:
		deps = o.addRequiredBinary("az", deps)
	case GKE:
		deps = o.addRequiredBinary("gcloud", deps)
	case MINIKUBE:
		deps = o.addRequiredBinary("minikube", deps)
	}
	return o.installMissingDependencies(deps)
}

func (o *CommonOptions) addRequiredBinary(binName string, deps []string) []string {
	d := binaryShouldBeInstalled(binName)
	if d != "" {
		deps = append(deps, d)
	}
	return deps
}
