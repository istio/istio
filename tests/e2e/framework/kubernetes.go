// Copyright 2017 Google Inc.
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
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/util"
)

const (
	yamlSuffix        = ".yaml"
	mixerHubDefault   = "MIXER_HUB"
	mixerTagDefault   = "MIXER_TAG"
	managerHubDefault = "MANAGER_HUB"
	managerTagDefault = "MANAGER_TAG"
)

var (
	// hub and tag is for app template if applicable TODO Find a better way to set default of these two
	appHubDefault = "gcr.io/istio-testing"
	appTagDefault = "b121a1e169365865e01a9e6eea066a34a29d9fd1"

	appHub     = flag.String("app_hub", appHubDefault, "app hub")
	appTag     = flag.String("app_tag", appTagDefault, "app tag")
	namespace  = flag.String("n", "", "Namespace to use for testing (empty to create/delete temporary one)")
	mixerHub   = flag.String("mixer_hub", os.Getenv(mixerHubDefault), "Mixer hub")
	mixerTag   = flag.String("mixer_tag", os.Getenv(mixerTagDefault), "Mixer tag")
	managerHub = flag.String("manager_hub", os.Getenv(managerHubDefault), "Manager hub")
	managerTag = flag.String("manager_tag", os.Getenv(managerTagDefault), "Manager tag")
	caHub      = flag.String("ca_hub", "", "Ca hub")
	caTag      = flag.String("ca_tag", "", "Ca tag")
	verbose    = flag.Bool("verbose", false, "Debug level noise from proxies")
)

// KubeInfo gathers information for kubectl
type KubeInfo struct {
	Namespace        string
	NamespaceCreated bool
	AppHub           string
	AppTag           string
	MixerHub         string
	MixerTag         string
	ManagerHub       string
	ManagerTag       string
	CaHub            string
	CaTag            string
	ProxyHub         string
	ProxyTag         string
	Verbosity        int

	TmpDir  string
	YamlDir string

	Ingress string

	Istioctl *util.Istioctl
}

// newKubeInfo create a new KubeInfo by given temp dir and runID
func newKubeInfo(tmpDir, runID string) *KubeInfo {
	if *namespace == "" {
		*namespace = runID
	}

	var verbosity int
	if *verbose {
		verbosity = 3
	} else {
		verbosity = 2
	}

	return &KubeInfo{
		Namespace:        *namespace,
		NamespaceCreated: false,
		AppHub:           *appHub,
		AppTag:           *appTag,
		MixerHub:         *mixerHub,
		MixerTag:         *mixerTag,
		ManagerHub:       *managerHub,
		ManagerTag:       *managerTag,
		CaHub:            *caHub,
		CaTag:            *caTag,
		ProxyHub:         *managerHub,
		ProxyTag:         *managerTag,
		Verbosity:        verbosity,
		TmpDir:           tmpDir,
		YamlDir:          filepath.Join(tmpDir, "yaml"),
		Istioctl:         util.NewIstioctl(tmpDir, *namespace),
	}
}

// Setup set up Kubernetes prerequest for tests
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

	if i, err := util.GetIngress(k.Namespace); err == nil {
		k.Ingress = i
	} else {
		return err
	}

	glog.Info("Kubernetes setup finished.")
	return nil
}

// Teardown clean up everything created by setup
func (k *KubeInfo) Teardown() error {
	if k.NamespaceCreated {
		if err := util.DeleteNamespace(k.Namespace); err != nil {
			return err
		}
		k.NamespaceCreated = false
		glog.Infof("Namespace %s deleted", k.Namespace)
	}

	glog.Flush()
	return nil
}

// Deploy istio modules
func (k *KubeInfo) deployIstio() error {
	if err := os.Mkdir(k.YamlDir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	if err := k.Istioctl.DownloadIstioctl(); err != nil {
		return err
	}

	if err := k.deployIstioCore("istio-manager.yaml"); err != nil {
		return err
	}
	if err := k.deployIstioCore("istio-mixer.yaml"); err != nil {
		return err
	}

	err := k.deployIstioCore("istio-ingress-controller.yaml")
	return err

	//Not using engress right now
	/*
		err := k.deployCore("egress-proxy.yaml")
		return err
	*/

}

// DeployIstioCore deploy istio module from yaml files
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

// DeployAppFromTmpl deploy testing app from tmpl
func (k *KubeInfo) DeployAppFromTmpl(deployment, svcName, port1, port2, port3, port4, version string, injectProxy bool) error {
	yamlFile := filepath.Join(k.YamlDir, svcName+"-app.yaml")
	if err := util.Fill(yamlFile, "app.yaml.tmpl", map[string]string{
		"Hub":        k.AppHub,
		"Tag":        k.AppTag,
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

// DeployAppFromYaml deploy testing app directly from yaml
func (k *KubeInfo) DeployAppFromYaml(src string, injectProxy bool) error {
	yamlFile := filepath.Join(k.YamlDir, path.Base(src))
	if err := util.CopyFile(util.GetTestRuntimePath(src), yamlFile); err != nil {
		return err
	}

	if err := k.deployApp(yamlFile, strings.TrimSuffix(path.Base(src), yamlSuffix), injectProxy); err != nil {
		return err
	}
	return nil
}

func (k *KubeInfo) deployApp(yamlFile, svcName string, injectProxy bool) error {
	if injectProxy {
		var err error
		if yamlFile, err = k.Istioctl.KubeInject(yamlFile, svcName, k.YamlDir, k.ProxyHub, k.ProxyTag); err != nil {
			return err
		}
	}

	if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
		glog.Errorf("Kubectl apply %s failed", yamlFile)
		return err
	}
	return nil
}
