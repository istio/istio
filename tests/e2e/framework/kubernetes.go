package framework

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strings"
	"path"
	"path/filepath"
	"os/exec"

	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/util"
)

const (
	VERSION_FILE = "istio.VERSION"
)

var (
	istioCtlUrlDefault string
	mixerHubDefault    string
	mixerTagDefault    string
	managerHubDefault  string
	managerTagDefault  string
)

type KubeInfo struct {
	Namespace        string
	NamespaceCreated bool
	Hub              string
	Tag              string
	MixerImage       string
	ManagerImage     string
	CaImage          string
	ProxyHub         string
	ProxyTag         string
	IstioCtlUrl      string
	IstioCtl         string
	Verbosity        int

	TmpDir  string
	YamlDir string
}

func NewKubeInfo(tmpDir, runId string) *KubeInfo {
	parseVersion()
	hubDefault := "gcr.io/istio-testing"
	tagDefault := "b121a1e169365865e01a9e6eea066a34a29d9fd1"

	hub := flag.String("hub", hubDefault, "Docker hub")
	tag := flag.String("tag", tagDefault, "Docker tag")
	namespace := flag.String("n", runId, "Namespace to use for testing (empty to create/delete temporary one)")
	mixerImage := flag.String("mixer", mixerHubDefault+"/mixer:"+mixerTagDefault, "Mixer image")
	managerImage := flag.String("manager", managerHubDefault+"/manager:"+managerTagDefault, "Manager image")
	caImage := flag.String("ca", "", "Ca image")
	proxyHub := flag.String("proxy_hub", managerHubDefault, "proxy hub")
	proxyTag := flag.String("proxy_tag", managerTagDefault, "proxy tag")
	istioCtlUrl := flag.String("istioctl_url", istioCtlUrlDefault, "URL to download istioctl")
	verbose := flag.Bool("verbose", false, "Debug level noise from proxies")

	var verbosity int
	if *verbose {
		verbosity = 3
	} else {
		verbosity = 2
	}

	return &KubeInfo{
		Hub:              *hub,
		Tag:              *tag,
		Namespace:        *namespace,
		MixerImage:       *mixerImage,
		ManagerImage:     *managerImage,
		CaImage:          *caImage,
		ProxyHub:         *proxyHub,
		ProxyTag:         *proxyTag,
		IstioCtlUrl:      *istioCtlUrl,
		Verbosity:        verbosity,
		NamespaceCreated: false,
		TmpDir:           tmpDir,
		YamlDir:          tmpDir + "/yaml",
	}
}

func (k *KubeInfo) SetUp() error {
	if err := util.CreateNamespace(k.Namespace); err != nil {
		glog.Error("Failed to create namespace.")
		return err
	}
	k.NamespaceCreated = true
	if err := k.deployIstio(); err != nil {
		glog.Error("Failed to deployIstio.")
		return err
	}

	glog.Info("Kubernetes setup finished.")
	return nil
}

func (k *KubeInfo) TearDown() error {
	if k.NamespaceCreated {
		if err := util.DeleteNamespace(k.Namespace); err != nil {
			return err
		}
		k.NamespaceCreated = false
		glog.Infof("Namespace %s deleted", k.Namespace)
	}
	glog.Info("Uploading log remotely")
	glog.Flush()
	return nil
}

// Deploy istio modules
func (k *KubeInfo) deployIstio() error {
	if err := os.Mkdir(k.YamlDir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}
	usr, eUser := user.Current()
	if eUser != nil {
		return eUser
	}
	homeDir := usr.HomeDir

	// Download Istioctl binary
	util.WebDownload(k.TmpDir+"/istioctl", k.IstioCtlUrl+"/istioctl-linux")
	if err := os.Chmod(fmt.Sprintf("%s/istioctl", k.TmpDir), 0755); err != nil {
		return err
	}
	k.IstioCtl = fmt.Sprintf("%s/istioctl -c %s/.kube/config", k.TmpDir, homeDir)
	glog.Infof("Downloaded istioctl to %s/istioctl\n", k.TmpDir)

	if err := k.deployCore("manager.yaml"); err != nil {
		return err
	}
	if err := k.deployCore("mixer.yaml"); err != nil {
		return err
	}
	if err := k.deployCore("ingress-proxy.yaml"); err != nil {
		return err
	}
	if err := k.deployCore("egress-proxy.yaml"); err != nil {
		return err
	}
	return nil
}

// Deploy istio module from yaml files
func (k *KubeInfo) deployCore(name string) error {
	yamlFile := k.TmpDir + "/yaml/" + name
	if err := util.Fill(yamlFile, name+".tmpl", *k); err != nil {
		glog.Errorf("Failed to fill %s", yamlFile)
		return err
	}
	if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
		glog.Errorf("Kubectl apply %s failed", yamlFile)
		return err
	}

	return nil
}

// Deploy testing app
func DeployApp(k *KubeInfo, deployment, svcName, port1, port2, port3, port4, version string, injectProxy bool) error {
	yamlFile := k.TmpDir + "/yaml/" + svcName + "-app.yaml"
	if err := util.Fill(yamlFile, "app.yaml.tmpl", map[string]string{
		"Hub":        k.Hub,
		"Tag":        k.Tag,
		"service":    svcName,
		"deployment": deployment,
		"port1":      port1,
		"port2":      port2,
		"port3":      port3,
		"port4":      port4,
		"version":    version,
	}); err != nil {
		glog.Errorf("Failed to generate yaml for service %s in deployment %s", svcName, deployment)
		return err
	}

	if injectProxy {
		injectedYamlFile := k.TmpDir + "/yaml/injected-" + svcName + "-app.yaml"
		if _, err := util.Shell(fmt.Sprintf("%s kube-inject -f %s -o %s --hub %s --tag %s -n %s", k.IstioCtl, yamlFile, injectedYamlFile, k.ProxyHub, k.ProxyTag, k.Namespace)); err != nil {
			glog.Errorf("Kube-inject failed for service %s in deployment %s", svcName, deployment)
			return err
		}
		yamlFile = injectedYamlFile
	}

	if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
		glog.Errorf("Kubectl apply %s failed", yamlFile)
		return err
	}
	return nil
}

// Parse default value from istio.VERSION, TODO: find a better way to do this
func parseVersion() {
	out, err := exec.Command("pwd").Output()
	glog.Infof("PWD: %s", out)

	ex, err := os.Executable()
	if err != nil {
			panic(err)
	}
	exPath := path.Dir(ex)
	glog.Infof("Get path: %s", exPath)

	file, err := os.Open(filepath.Join(util.GetTestRuntimePath(), VERSION_FILE))
	if err != nil {
		glog.Warningf("Cannot get version file %s : %s\n", VERSION_FILE, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if m, _ := regexp.MatchString("^export (..*)=(..*)", line); m {
			parts := strings.Split(line[7:], "=")
			switch parts[0] {
			case "MIXER_HUB":
				mixerHubDefault = (parts[1])[1:(len(parts[1]) - 1)]
			case "MIXER_TAG":
				mixerTagDefault = (parts[1])[1:(len(parts[1]) - 1)]
			case "MANAGER_HUB":
				managerHubDefault = (parts[1])[1:(len(parts[1]) - 1)]
			case "MANAGER_TAG":
				managerTagDefault = (parts[1])[1:(len(parts[1]) - 1)]
			case "ISTIOCTL_URL":
				istioCtlUrlDefault = (parts[1])[1:(len(parts[1]) - 1)]
			}
		}
	}

	if err := scanner.Err(); err != nil {
		glog.Warningf("Scanning version file %s failed : %s\n", VERSION_FILE, err)
	}
}
