package framework

import (
	"bufio"
	"flag"
	"os"
	"regexp"
	"strings"
	"path"

	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/util"
)

const (
	VERSION_FILE = "istio.VERSION"
	YAML_SUFFIX = ".yaml"
)

var (
	kube KubeInfo
	istioctlUrlDefault string
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
	IstioctlUrl      string
	Istioctl         string
	Verbosity        int

	TmpDir  string
	YamlDir string
}

func init() {
	parseVersion()

	hubDefault := "gcr.io/istio-testing"
	tagDefault := "b121a1e169365865e01a9e6eea066a34a29d9fd1"
	var verbose bool

	flag.StringVar(&kube.Hub, "hub", hubDefault, "Docker hub")
	flag.StringVar(&kube.Tag, "tag", tagDefault, "Docker tag")
	flag.StringVar(&kube.Namespace, "n", "", "Namespace to use for testing (empty to create/delete temporary one)")
	flag.StringVar(&kube.MixerImage, "mixer", mixerHubDefault+"/mixer:"+mixerTagDefault, "Mixer image")
	flag.StringVar(&kube.ManagerImage, "manager", managerHubDefault+"/manager:"+managerTagDefault, "Manager image")
	flag.StringVar(&kube.CaImage, "ca", "", "Ca image")
	flag.StringVar(&kube.ProxyHub, "proxy_hub", managerHubDefault, "proxy hub")
	flag.StringVar(&kube.ProxyTag, "proxy_tag", managerTagDefault, "proxy tag")
	flag.StringVar(&kube.IstioctlUrl, "istioctl_url", istioctlUrlDefault, "URL to download istioctl")
	flag.BoolVar(&verbose, "verbose", false, "Debug level noise from proxies")

	if verbose {
		kube.Verbosity = 3
	} else {
		kube.Verbosity = 2
	}
}

func NewKubeInfo(tmpDir, runId string) *KubeInfo {
	if kube.Namespace == "" {
		kube.Namespace = runId
	}

	kube.NamespaceCreated = false
	kube.TmpDir = tmpDir
	kube.YamlDir = tmpDir + "/yaml/"

	return &kube
}

func (k *KubeInfo) Setup() error {
	if err := util.CreateNamespace(k.Namespace); err != nil {
		glog.Error("Failed to create namespace.")
		return err
	}
	k.NamespaceCreated = true
	if err := k.deployIstio(); err != nil {
		glog.Error("Failed to deployIstio.")
		return err
	}

	util.GetGateway(k.Namespace)

	glog.Info("Kubernetes setup finished.")
	return nil
}

func (k *KubeInfo) Teardown() error {
	if k.NamespaceCreated {
	//	if err := util.DeleteNamespace(k.Namespace); err != nil {
	//		return err
	//	}
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

	if istioctl, err := util.DownloadIstioctl(k.TmpDir, k.IstioctlUrl); err != nil {
		return err
	} else {
		k.Istioctl = istioctl
	}

	if err := k.deployIstioCore("manager.yaml"); err != nil {
		return err
	}
	if err := k.deployIstioCore("mixer.yaml"); err != nil {
		return err
	}
	if err := k.deployIstioCore("ingress-proxy.yaml"); err != nil {
		return err
	}
	/* Not using engress right now
	if err := k.deployCore("egress-proxy.yaml"); err != nil {
		return err
	}
	*/
	return nil
}

// Deploy istio module from yaml files
func (k *KubeInfo) deployIstioCore(name string) error {
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

// Deploy testing app from tmpl
func (k *KubeInfo) DeployAppFromTmpl(deployment, svcName, port1, port2, port3, port4, version string, injectProxy bool) error {
	yamlFile := k.YamlDir + svcName + "-app.yaml"
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

	if err := k.deployApp(yamlFile, svcName, injectProxy); err != nil {
		return err
	}
	return nil
}

// Deploy testing app directly from yaml
func (k *KubeInfo) DeployAppFromYaml(src string, injectProxy bool) error {
	yamlFile := k.YamlDir + path.Base(src)
	if err := util.CopyFile(util.GetTestRuntimePath(src), yamlFile); err != nil {
		return err
	}

	if err := k.deployApp(yamlFile, strings.TrimSuffix(path.Base(src), YAML_SUFFIX), injectProxy); err != nil {
		return err
	}
	return nil
}

func (k *KubeInfo) deployApp(yamlFile, svcName string, injectProxy bool) error {
	if injectProxy {
		var err error
		if yamlFile, err = util.KubeInject(yamlFile, svcName, k.YamlDir, k.Istioctl, k.ProxyHub, k.ProxyTag, k.Namespace); err != nil {
			return err
		}
	}

	if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
		glog.Errorf("Kubectl apply %s failed", yamlFile)
		return err
	}
	return nil
}

// Parse default value from istio.VERSION, TODO: find a better way to do this
func parseVersion() {
	file, err := os.Open(util.GetTestRuntimePath(VERSION_FILE))
	if err != nil {
		glog.Warningf("Cannot get version file %s : %s\n", VERSION_FILE, err)
		return
	}
	defer file.Close()

	r := regexp.MustCompile("^export (..*)=(..*)")
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if s := r.FindStringSubmatch(line); len(s) == 3 {
			switch s[1] {
			case "MIXER_HUB":
				mixerHubDefault = strings.Trim(s[2], "\"")
			case "MIXER_TAG":
				mixerTagDefault = strings.Trim(s[2], "\"")
			case "MANAGER_HUB":
				managerHubDefault = strings.Trim(s[2], "\"")
			case "MANAGER_TAG":
				managerTagDefault = strings.Trim(s[2], "\"")
			case "ISTIOCTL_URL":
				istioctlUrlDefault = strings.Trim(s[2], "\"")
			}
		}
	}

	if err := scanner.Err(); err != nil {
		glog.Warningf("Scanning version file %s failed : %s\n", VERSION_FILE, err)
	}
}
