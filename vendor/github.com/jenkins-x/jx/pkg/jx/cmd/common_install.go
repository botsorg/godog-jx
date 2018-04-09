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
		case "kubectl":
			err = o.installKubectl()
		case "hyperkit":
			err = o.installHyperkit()
		case "xhyve":
			err = o.installXhyve()
		case "virtualbox":
			err = o.installVirtualBox()
		case "helm":
			err = o.installHelm()
		case "gcloud":
			err = o.installGcloud()
		case "kops":
			err = o.installKops()
		case "minikube":
			err = o.installMinikube()
		case "az":
			err = o.installAzureCli()
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
