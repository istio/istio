// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package framework

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

const (
	yamlSuffix                  = ".yaml"
	istioInstallDir             = "install/kubernetes"
	istioAddonsDir              = "install/kubernetes/addons"
	nonAuthInstallFile          = "istio.yaml"
	authInstallFile             = "istio-auth.yaml"
	nonAuthInstallFileNamespace = "istio-one-namespace.yaml"
	authInstallFileNamespace    = "istio-one-namespace-auth.yaml"
	istioSystem                 = "istio-system"
	defaultSidecarInjectorFile  = "istio-sidecar-injector.yaml"
)

var (
	namespace           = flag.String("namespace", "", "Namespace to use for testing (empty to create/delete temporary one)")
	mixerHub            = flag.String("mixer_hub", os.Getenv("HUB"), "Mixer hub")
	mixerTag            = flag.String("mixer_tag", os.Getenv("TAG"), "Mixer tag")
	pilotHub            = flag.String("pilot_hub", os.Getenv("HUB"), "Pilot hub")
	pilotTag            = flag.String("pilot_tag", os.Getenv("TAG"), "Pilot tag")
	proxyHub            = flag.String("proxy_hub", os.Getenv("HUB"), "Proxy hub")
	proxyTag            = flag.String("proxy_tag", os.Getenv("TAG"), "Proxy tag")
	caHub               = flag.String("ca_hub", os.Getenv("HUB"), "Ca hub")
	caTag               = flag.String("ca_tag", os.Getenv("TAG"), "Ca tag")
	authEnable          = flag.Bool("auth_enable", false, "Enable auth")
	localCluster        = flag.Bool("use_local_cluster", false, "Whether the cluster is local or not")
	skipSetup           = flag.Bool("skip_setup", false, "Skip namespace creation and istio cluster setup")
	sidecarInjectorFile = flag.String("sidecar_injector_file", defaultSidecarInjectorFile, "Sidecar injector yaml file")
	clusterWide         = flag.Bool("cluster_wide", false, "Run cluster wide tests")

	addons = []string{
		"prometheus",
		"zipkin",
	}
)

// KubeInfo gathers information for kubectl
type KubeInfo struct {
	Namespace string

	TmpDir  string
	yamlDir string

	Ingress string

	localCluster     bool
	namespaceCreated bool
	AuthEnabled      bool

	// Istioctl installation
	Istioctl *Istioctl
	// App Manager
	AppManager *AppManager

	// Release directory
	ReleaseDir string
	// Use baseversion if not empty.
	BaseVersion string
}

// newKubeInfo create a new KubeInfo by given temp dir and runID
// If baseVersion is not empty, will use the specified release of Istio instead of the local one.
func newKubeInfo(tmpDir, runID, baseVersion string) (*KubeInfo, error) {
	if *namespace == "" {
		if *clusterWide {
			*namespace = istioSystem
		} else {
			*namespace = runID
		}
	}
	yamlDir := filepath.Join(tmpDir, "yaml")
	i, err := NewIstioctl(yamlDir, *namespace, *namespace, *proxyHub, *proxyTag)
	if err != nil {
		return nil, err
	}
	a := NewAppManager(tmpDir, *namespace, i)
	// Download the base release if baseVersion is specified.
	var releaseDir string
	if baseVersion != "" {
		releaseDir, err = util.DownloadRelease(baseVersion, tmpDir)
		if err != nil {
			return nil, err
		}
		// Use istioctl from base version to inject the sidecar.
		i.localPath = filepath.Join(releaseDir, "/bin/istioctl")
		os.Chmod(i.localPath, 0755)
	} else {
		releaseDir = util.GetResourcePath("")
	}
	log.Infof("Using release dir: %s", releaseDir)
	return &KubeInfo{
		Namespace:        *namespace,
		namespaceCreated: false,
		TmpDir:           tmpDir,
		yamlDir:          yamlDir,
		localCluster:     *localCluster,
		Istioctl:         i,
		AppManager:       a,
		AuthEnabled:      *authEnable,
		ReleaseDir:       releaseDir,
		BaseVersion:      baseVersion,
	}, nil
}

// Setup set up Kubernetes prerequest for tests
func (k *KubeInfo) Setup() error {
	log.Info("Setting up kubeInfo")
	var err error
	if err = os.Mkdir(k.yamlDir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	if !*skipSetup {
		if err = k.deployIstio(); err != nil {
			log.Error("Failed to deploy Istio.")
			return err
		}

		if err = k.deployAddons(); err != nil {
			log.Error("Failed to deploy istio addons")
			return err
		}
	}

	var in string
	if k.localCluster {
		in, err = util.GetIngressPod(k.Namespace)
	} else {
		in, err = util.GetIngress(k.Namespace)
	}
	if err != nil {
		return err
	}
	k.Ingress = in
	return nil
}

// Teardown clean up everything created by setup
func (k *KubeInfo) Teardown() error {
	log.Info("Cleaning up kubeInfo")

	if *skipSetup || *skipCleanup {
		return nil
	}

	if *useAutomaticInjection {
		testSidecarInjectorYAML := filepath.Join(k.TmpDir, "yaml", *sidecarInjectorFile)

		if err := util.KubeDelete(k.Namespace, testSidecarInjectorYAML); err != nil {
			log.Errorf("Istio sidecar injector %s deletion failed", testSidecarInjectorYAML)
			return err
		}
	}

	if *clusterWide {
		// for cluster-wide, we can verify the uninstall
		istioYaml := nonAuthInstallFile
		if *authEnable {
			istioYaml = authInstallFile
		}

		testIstioYaml := filepath.Join(k.TmpDir, "yaml", istioYaml)

		if err := util.KubeDelete(k.Namespace, testIstioYaml); err != nil {
			log.Infof("Safe to ignore resource not found errors in kubectl delete -f %s", testIstioYaml)
		}
	} else {
		if err := util.DeleteNamespace(k.Namespace); err != nil {
			log.Errorf("Failed to delete namespace %s", k.Namespace)
			return err
		}

		// ClusterRoleBindings are not namespaced and need to be deleted separately
		if _, err := util.Shell("kubectl get clusterrolebinding -o jsonpath={.items[*].metadata.name}"+
			"|xargs -n 1|fgrep %s|xargs kubectl delete clusterrolebinding",
			k.Namespace); err != nil {
			log.Errorf("Failed to delete clusterrolebindings associated with namespace %s", k.Namespace)
			return err
		}

		// ClusterRoles are not namespaced and need to be deleted separately
		if _, err := util.Shell("kubectl get clusterrole -o jsonpath={.items[*].metadata.name}"+
			"|xargs -n 1|fgrep %s|xargs kubectl delete clusterrole",
			k.Namespace); err != nil {
			log.Errorf("Failed to delete clusterroles associated with namespace %s", k.Namespace)
			return err
		}
	}

	// confirm the namespace is deleted as it will cause future creation to fail
	maxAttempts := 30
	namespaceDeleted := false
	for attempts := 1; attempts <= maxAttempts; attempts++ {
		namespaceDeleted, _ = util.NamespaceDeleted(k.Namespace)
		if namespaceDeleted {
			break
		}
		time.Sleep(4 * time.Second)
	}

	if !namespaceDeleted {
		log.Errorf("Failed to delete namespace %s after %v seconds", k.Namespace, maxAttempts*4)
		return nil
	}

	log.Infof("Namespace %s deletion status: %v", k.Namespace, namespaceDeleted)

	return nil
}

func (k *KubeInfo) deployAddons() error {
	for _, addon := range addons {
		addonPath := filepath.Join(istioAddonsDir, fmt.Sprintf("%s.yaml", addon))
		baseYamlFile := filepath.Join(k.ReleaseDir, addonPath)
		content, err := ioutil.ReadFile(baseYamlFile)
		if err != nil {
			log.Errorf("Cannot read file %s", baseYamlFile)
			return err
		}

		if !*clusterWide {
			content = replacePattern(k, content, istioSystem, k.Namespace)
		}

		yamlFile := filepath.Join(k.TmpDir, "yaml", addon+".yaml")
		err = ioutil.WriteFile(yamlFile, content, 0600)
		if err != nil {
			log.Errorf("Cannot write into file %s", yamlFile)
		}

		if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
			log.Errorf("Kubectl apply %s failed", yamlFile)
			return err
		}
	}
	return nil
}

func (k *KubeInfo) deployIstio() error {
	istioYaml := nonAuthInstallFileNamespace
	if *clusterWide {
		if *authEnable {
			istioYaml = authInstallFile
		} else {
			istioYaml = nonAuthInstallFile
		}
	} else {
		if *authEnable {
			istioYaml = authInstallFileNamespace
		}
	}
	yamlDir := filepath.Join(istioInstallDir, istioYaml)
	baseIstioYaml := filepath.Join(k.ReleaseDir, yamlDir)
	testIstioYaml := filepath.Join(k.TmpDir, "yaml", istioYaml)

	if err := k.generateIstio(baseIstioYaml, testIstioYaml); err != nil {
		log.Errorf("Generating yaml %s failed", testIstioYaml)
		return err
	}
	if err := util.KubeApply(k.Namespace, testIstioYaml); err != nil {
		log.Errorf("Istio core %s deployment failed", testIstioYaml)
		return err
	}

	if *useAutomaticInjection {
		baseSidecarInjectorYAML := util.GetResourcePath(filepath.Join(istioInstallDir, *sidecarInjectorFile))
		testSidecarInjectorYAML := filepath.Join(k.TmpDir, "yaml", *sidecarInjectorFile)
		if err := k.generateSidecarInjector(baseSidecarInjectorYAML, testSidecarInjectorYAML); err != nil {
			log.Errorf("Generating sidecar injector yaml failed")
			return err
		}
		if err := util.KubeApply(k.Namespace, testSidecarInjectorYAML); err != nil {
			log.Errorf("Istio sidecar injector %s deployment failed", testSidecarInjectorYAML)
			return err
		}

		// alow time for sidecar injector to start
		time.Sleep(60 * time.Second)
	}

	return nil
}

func updateInjectImage(name, module, hub, tag string, content []byte) []byte {
	image := []byte(fmt.Sprintf("%s: %s/%s:%s", name, hub, module, tag))
	r := regexp.MustCompile(fmt.Sprintf("%s: .*(\\/%s):.*", name, module))
	return r.ReplaceAllLiteral(content, image)
}

func updateInjectVersion(version string, content []byte) []byte {
	versionLine := []byte(fmt.Sprintf("version: %s", version))
	r := regexp.MustCompile("version: .*")
	return r.ReplaceAllLiteral(content, versionLine)
}

func (k *KubeInfo) generateSidecarInjector(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	if !*clusterWide {
		content = replacePattern(k, content, istioSystem, k.Namespace)
	}

	if *pilotHub != "" && *pilotTag != "" {
		content = updateIstioYaml("sidecar_injector", *pilotHub, *pilotTag, content)
		content = updateInjectVersion(*pilotTag, content)
		content = updateInjectImage("initImage", "proxy_init", *pilotHub, *pilotTag, content)
		content = updateInjectImage("proxyImage", "proxy", *proxyHub, *proxyTag, content)
	}

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		log.Errorf("Cannot write into generate sidecar injector file %s", dst)
	}
	return err
}

func replacePattern(k *KubeInfo, content []byte, src, dest string) []byte {
	r := []byte(dest)
	p := regexp.MustCompile(src)
	content = p.ReplaceAllLiteral(content, r)
	return content
}

func (k *KubeInfo) generateIstio(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	if !*clusterWide {
		content = replacePattern(k, content, istioSystem, k.Namespace)
		// Customize mixer's configStoreURL to limit watching resources in the testing namespace.
		vs := url.Values{}
		vs.Add("ns", *namespace)
		content = replacePattern(k, content, "--configStoreURL=k8s://", "--configStoreURL=k8s://?"+vs.Encode())
	}

	// Replace long refresh delays with short ones for the sake of tests.
	content = replacePattern(k, content, "connectTimeout: 10s", "connectTimeout: 1s")
	content = replacePattern(k, content, "drainDuration: 45s", "drainDuration: 2s")
	content = replacePattern(k, content, "parentShutdownDuration: 1m0s", "parentShutdownDuration: 3s")

	// A very flimsy and unreliable regexp to replace delays in ingress pod Spec
	content = replacePattern(k, content, "'30s' #discoveryRefreshDelay", "'1s' #discoveryRefreshDelay")
	content = replacePattern(k, content, "'10s' #connectTimeout", "'1s' #connectTimeout")
	content = replacePattern(k, content, "'45s' #drainDuration", "'2s' #drainDuration")
	content = replacePattern(k, content, "'1m0s' #parentShutdownDuration", "'3s' #parentShutdownDuration")

	if k.BaseVersion == "" {
		if *mixerHub != "" && *mixerTag != "" {
			content = updateIstioYaml("mixer", *mixerHub, *mixerTag, content)
		}
		if *pilotHub != "" && *pilotTag != "" {
			content = updateIstioYaml("pilot", *pilotHub, *pilotTag, content)
		}
		if *proxyHub != "" && *proxyTag != "" {
			//Need to be updated when the string "proxy" is changed as the default image name
			content = updateIstioYaml("proxy", *proxyHub, *proxyTag, content)
		}
		if *caHub != "" && *caTag != "" {
			//Need to be updated when the string "istio-ca" is changed
			content = updateIstioYaml("istio-ca", *caHub, *caTag, content)
		}
	}
	if *localCluster {
		content = []byte(strings.Replace(string(content), "LoadBalancer", "NodePort", 1))
	}

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		log.Errorf("Cannot write into generated yaml file %s", dst)
	}
	return err
}

func updateIstioYaml(module, hub, tag string, content []byte) []byte {
	image := []byte(fmt.Sprintf("image: %s/%s:%s", hub, module, tag))
	r := regexp.MustCompile(fmt.Sprintf("image: .*(\\/%s):.*", module))
	return r.ReplaceAllLiteral(content, image)
}
