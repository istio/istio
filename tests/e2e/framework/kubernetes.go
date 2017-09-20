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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/golang/glog"

	"istio.io/istio/tests/e2e/util"
)

const (
	yamlSuffix         = ".yaml"
	istioInstallDir    = "install/kubernetes"
	istioAddonsDir     = "install/kubernetes/addons"
	nonAuthInstallFile = "istio.yaml"
	authInstallFile    = "istio-auth.yaml"
	istioSystem        = "istio-system"
	mixerConfigDefault = "istio-config-default"
)

var (
	namespace       = flag.String("namespace", "", "Namespace to use for testing (empty to create/delete temporary one)")
	mixerHub        = flag.String("mixer_hub", "", "Mixer hub, if different from istio.Version")
	mixerTag        = flag.String("mixer_tag", "", "Mixer tag, if different from istio.Version")
	pilotHub        = flag.String("pilot_hub", "", "pilot hub, if different from istio.Version")
	pilotTag        = flag.String("pilot_tag", "", "pilot tag, if different from istio.Version")
	caHub           = flag.String("ca_hub", "", "Ca hub")
	caTag           = flag.String("ca_tag", "", "Ca tag")
	authEnable      = flag.Bool("auth_enable", false, "Enable auth")
	rbacfile        = flag.String("rbac_path", "", "Rbac yaml file")
	localCluster    = flag.Bool("use_local_cluster", false, "Whether the cluster is local or not")
	skipSetup       = flag.Bool("skip_setup", false, "Skip namespace creation and istio cluster setup")
	initializerFile = flag.String("initializer_file", "", "Initializer yaml file")

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

	// Istioctl installation
	Istioctl *Istioctl
	// App Manager
	AppManager *AppManager
}

// newKubeInfo create a new KubeInfo by given temp dir and runID
func newKubeInfo(tmpDir, runID string) (*KubeInfo, error) {
	if *namespace == "" {
		*namespace = runID
	}
	yamlDir := filepath.Join(tmpDir, "yaml")
	i, err := NewIstioctl(yamlDir, *namespace, *namespace, *pilotHub, *pilotTag)
	if err != nil {
		return nil, err
	}
	a := NewAppManager(tmpDir, *namespace, i)

	return &KubeInfo{
		Namespace:        *namespace,
		namespaceCreated: false,
		TmpDir:           tmpDir,
		yamlDir:          yamlDir,
		localCluster:     *localCluster,
		Istioctl:         i,
		AppManager:       a,
	}, nil
}

// Setup set up Kubernetes prerequest for tests
func (k *KubeInfo) Setup() error {
	glog.Info("Setting up kubeInfo")
	var err error
	if err = os.Mkdir(k.yamlDir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	if !*skipSetup {
		if err = util.CreateNamespace(k.Namespace); err != nil {
			glog.Error("Failed to create namespace.")
			return err
		}
		k.namespaceCreated = true

		if err = k.deployIstio(); err != nil {
			glog.Error("Failed to deploy Istio.")
			return err
		}

		if err = k.deployAddons(); err != nil {
			glog.Error("Failed to deploy istio addons")
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
	glog.Info("Cleaning up kubeInfo")
	var err error

	if *rbacfile != "" {

		testRbacYaml := filepath.Join(k.TmpDir, "yaml", filepath.Base(*rbacfile))
		if _, err = os.Stat(testRbacYaml); os.IsNotExist(err) {
			glog.Errorf("%s File does not exist", testRbacYaml)
		} else if err = util.KubeDelete(k.Namespace, testRbacYaml); err != nil {
			glog.Errorf("Rbac deletion failed, please remove stale ClusterRoleBindings")
		}
	}

	if k.namespaceCreated {
		if err = util.DeleteNamespace(k.Namespace); err != nil {
			glog.Errorf("Failed to delete namespace %s", k.Namespace)
			return err
		}

		// confirm the namespace is deleted as it will cause future creation to fail
		maxAttempts := 15
		namespaceDeleted := false
		totalWait := 0
		for attempts := 1; attempts <= maxAttempts; attempts++ {
			namespaceDeleted, err = util.NamespaceDeleted(k.Namespace)
			if namespaceDeleted {
				break
			}
			totalWait += attempts
			time.Sleep(time.Duration(attempts) * time.Second)
		}

		if !namespaceDeleted {
			glog.Errorf("Failed to delete namespace %s after %v seconds", k.Namespace, totalWait)
			return err
		}
		k.namespaceCreated = false
		glog.Infof("Namespace %s deletion status: %v", k.Namespace, namespaceDeleted)
	}
	return err
}

func (k *KubeInfo) deployAddons() error {
	for _, addon := range addons {

		baseYamlFile := util.GetResourcePath(filepath.Join(istioAddonsDir, fmt.Sprintf("%s.yaml", addon)))

		content, err := ioutil.ReadFile(baseYamlFile)
		if err != nil {
			glog.Errorf("Cannot read file %s", baseYamlFile)
			return err
		}

		content = replacePattern(k, content, istioSystem, k.Namespace)

		yamlFile := filepath.Join(k.TmpDir, "yaml", addon+".yaml")
		err = ioutil.WriteFile(yamlFile, content, 0600)
		if err != nil {
			glog.Errorf("Cannot write into file %s", yamlFile)
		}

		if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
			glog.Errorf("Kubectl apply %s failed", yamlFile)
			return err
		}
	}
	return nil
}

func (k *KubeInfo) deployIstio() error {
	istioYaml := nonAuthInstallFile
	if *authEnable {
		istioYaml = authInstallFile
	}
	baseIstioYaml := util.GetResourcePath(filepath.Join(istioInstallDir, istioYaml))
	testIstioYaml := filepath.Join(k.TmpDir, "yaml", istioYaml)

	if *rbacfile != "" {
		baseRbacYaml := util.GetResourcePath(*rbacfile)
		testRbacYaml := filepath.Join(k.TmpDir, "yaml", filepath.Base(*rbacfile))
		if err := k.generateRbac(baseRbacYaml, testRbacYaml); err != nil {
			glog.Errorf("Generating rbac yaml failed")
		}
		if err := util.KubeApply(k.Namespace, testRbacYaml); err != nil {
			glog.Errorf("Rbac deployment failed")
			return err
		}
	}

	if *useInitializer {
		baseInitializerYAML := util.GetResourcePath(*initializerFile)
		testInitializerYAML := filepath.Join(k.TmpDir, "yaml", filepath.Base(*initializerFile))
		if err := k.generateInitializer(baseInitializerYAML, testInitializerYAML); err != nil {
			glog.Errorf("Generating initializer yaml failed")
			return err
		}
		if err := util.KubeApply(k.Namespace, testInitializerYAML); err != nil {
			glog.Errorf("Istio sidecar initializer %s deployment failed", testInitializerYAML)
			return err
		}
	}

	if err := k.generateIstio(baseIstioYaml, testIstioYaml); err != nil {
		glog.Errorf("Generating yaml %s failed", testIstioYaml)
		return err
	}
	if err := util.KubeApply(k.Namespace, testIstioYaml); err != nil {
		glog.Errorf("Istio core %s deployment failed", testIstioYaml)
		return err
	}

	return nil
}

func (k *KubeInfo) generateRbac(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		glog.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	content = replacePattern(k, content, istioSystem, k.Namespace)
	content = replacePattern(k, content, "namespace: default",
		"namespace: "+k.Namespace)

	content = replacePattern(k, content, "istio-pilot-admin-role-binding",
		"istio-pilot-admin-role-binding-"+k.Namespace)

	content = replacePattern(k, content, "istio-mixer-admin-role-binding",
		"istio-mixer-admin-role-binding-"+k.Namespace)

	content = replacePattern(k, content, "istio-ca-role-binding",
		"istio-ca-role-binding-"+k.Namespace)

	content = replacePattern(k, content, "istio-ingress-admin-role-binding",
		"istio-ingress-admin-role-binding-"+k.Namespace)

	content = replacePattern(k, content, "istio-egress-admin-role-binding",
		"istio-egress-admin-role-binding-"+k.Namespace)

	content = replacePattern(k, content, "istio-sidecar-role-binding",
		"istio-sidecar-role-binding-"+k.Namespace)

	content = replacePattern(k, content, "istio-initializer-admin-role-binding",
		"istio-initializer-admin-role-binding-"+k.Namespace)

	content = replacePattern(k, content, mixerConfigDefault, k.Namespace)

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		glog.Errorf("Cannot write into generate rbac file %s", dst)
	}
	return err
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

func (k *KubeInfo) generateInitializer(src, dst string) error {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		glog.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	content = replacePattern(k, content, istioSystem, k.Namespace)

	if *pilotHub != "" && *pilotTag != "" {
		content = updateIstioYaml("initializer", *pilotHub, *pilotTag, content)
		content = updateInjectVersion(*pilotTag, content)
		content = updateInjectImage("initImage", "proxy_init", *pilotHub, *pilotTag, content)
		content = updateInjectImage("proxyImage", "proxy", *pilotHub, *pilotTag, content)
	}

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		glog.Errorf("Cannot write into generate initializer file %s", dst)
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
		glog.Errorf("Cannot read original yaml file %s", src)
		return err
	}

	content = replacePattern(k, content, istioSystem, k.Namespace)
	content = replacePattern(k, content, mixerConfigDefault, k.Namespace)

	// Replace long refresh delays with short ones for the sake of tests.
	content = replacePattern(k, content, "rdsRefreshDelay: 30s", "rdsRefreshDelay: 1s")
	content = replacePattern(k, content, "discoveryRefreshDelay: 30s", "discoveryRefreshDelay: 1s")
	content = replacePattern(k, content, "connectTimeout: 10s", "connectTimeout: 1s")
	content = replacePattern(k, content, "drainDuration: 45s", "drainDuration: 2s")
	content = replacePattern(k, content, "parentShutdownDuration: 1m0s", "parentShutdownDuration: 3s")

	if *mixerHub != "" && *mixerTag != "" {
		content = updateIstioYaml("mixer", *mixerHub, *mixerTag, content)
	}
	if *pilotHub != "" && *pilotTag != "" {
		content = updateIstioYaml("pilot", *pilotHub, *pilotTag, content)
		//Need to be updated when the string "proxy_debug" is changed
		content = updateIstioYaml("proxy_debug", *pilotHub, *pilotTag, content)
	}
	if *caHub != "" && *caTag != "" {
		//Need to be updated when the string "istio-ca" is changed
		content = updateIstioYaml("istio-ca", *caHub, *caTag, content)
	}
	if *localCluster {
		content = []byte(strings.Replace(string(content), "LoadBalancer", "NodePort", 1))
	}

	content = []byte(strings.Replace(string(content),
		`args: ["discovery", "-v", "2", "--admission-service", "istio-pilot-external"]`,
		`args: ["discovery", "-v", "2", "--admission-service", "istio-pilot-external", "-a", "`+k.Namespace+`"]`,
		-1))

	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		glog.Errorf("Cannot write into generated yaml file %s", dst)
	}
	return err
}

func updateIstioYaml(module, hub, tag string, content []byte) []byte {
	image := []byte(fmt.Sprintf("image: %s/%s:%s", hub, module, tag))
	r := regexp.MustCompile(fmt.Sprintf("image: .*(\\/%s):.*", module))
	return r.ReplaceAllLiteral(content, image)
}
