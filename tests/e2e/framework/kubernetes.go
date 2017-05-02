// Copyright 2017 Istio Inc.
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

	"github.com/golang/glog"

	"istio.io/istio/tests/e2e/util"
)

const (
	yamlSuffix       = ".yaml"
	mixerHubEnvVar   = "MIXER_HUB"
	mixerTagEnvVar   = "MIXER_TAG"
	managerHubEnvVar = "MANAGER_HUB"
	managerTagEnvVar = "MANAGER_TAG"
	istioInstallDir  = "kubernetes/istio-install"
)

var (
	namespace  = flag.String("namespace", "", "Namespace to use for testing (empty to create/delete temporary one)")
	mixerHub   = flag.String("mixer_hub", os.Getenv(mixerHubEnvVar), "Mixer hub")
	mixerTag   = flag.String("mixer_tag", os.Getenv(mixerTagEnvVar), "Mixer tag")
	managerHub = flag.String("manager_hub", os.Getenv(managerHubEnvVar), "Manager hub")
	managerTag = flag.String("manager_tag", os.Getenv(managerTagEnvVar), "Manager tag")
	caHub      = flag.String("ca_hub", "", "Ca hub")
	caTag      = flag.String("ca_tag", "", "Ca tag")
	verbose    = flag.Bool("verbose", false, "Debug level noise from proxies")

	modules = []string{
		"manager",
		"mixer",
		"ingress",
	}
)

// KubeInfo gathers information for kubectl
type KubeInfo struct {
	Namespace        string
	namespaceCreated bool
	MixerImage       string
	ManagerImage     string
	CaImage          string
	ProxyImage       string
	Verbosity        int

	TmpDir  string
	yamlDir string

	Ingress string

	Istioctl *Istioctl

	Apps []AppInterface
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
		namespaceCreated: false,
		MixerImage:       fmt.Sprintf("%s/mixer:%s", *mixerHub, *mixerTag),
		ManagerImage:     fmt.Sprintf("%s/manager:%s", *managerHub, *managerTag),
		CaImage:          fmt.Sprintf("%s/ca:%s", *caHub, *caTag),
		// Proxy and Manager are released together and share the same hub and tag.
		ProxyImage: fmt.Sprintf("%s/proxy:%s", *managerHub, *managerTag),
		Verbosity:  verbosity,
		TmpDir:     tmpDir,
		yamlDir:    filepath.Join(tmpDir, "yaml"),
		Istioctl:   NewIstioctl(tmpDir, *namespace, *managerHub, *managerTag),
	}
}

// Setup set up Kubernetes prerequest for tests
func (k *KubeInfo) Setup() error {
	var err error
	if err = os.Mkdir(k.yamlDir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}

	if err = k.Istioctl.Install(); err != nil {
		return err
	}

	if err = util.CreateNamespace(k.Namespace); err != nil {
		glog.Error("Failed to create namespace.")
		return err
	}
	k.namespaceCreated = true

	if err = k.deployIstio(); err != nil {
		glog.Error("Failed to deployIstio.")
		return err
	}

	var in string
	if in, err = util.GetIngress(k.Namespace); err != nil {
		return err
	}
	k.Ingress = in

	if err = os.Setenv("ISTIO_MANAGER_ADDRESS", "http://localhost:8081"); err != nil {
		return err
	}

	if err = k.deployApps(); err != nil {
		glog.Error("Failed to deploy apps")
		return err
	}

	glog.Info("Kubernetes setup finished.")
	return nil
}

// Teardown clean up everything created by setup
func (k *KubeInfo) Teardown() error {
	var err error
	if k.namespaceCreated {
		if err = util.DeleteNamespace(k.Namespace); err != nil {
			glog.Error("Failed to delete namespace")
			return err
		}
		k.namespaceCreated = false
		glog.Infof("Namespace %s deleted", k.Namespace)
	}
	return err
}

func (k *KubeInfo) deployIstio() error {
	for _, module := range modules {
		if err := k.deployIstioCore(module); err != nil {
			glog.Infof("Failed to deploy %s", module)
			return err
		}
	}
	return nil
}

// DeployIstioCore deploy istio module from yaml files
func (k *KubeInfo) deployIstioCore(module string) error {
	yamlFile := filepath.Join(k.TmpDir, "yaml", fmt.Sprintf("istio-%s.yaml", module))
	if err := k.generateIstioCore(yamlFile, module); err != nil {
		return err
	}
	if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
		glog.Errorf("Kubectl apply %s failed", yamlFile)
		return err
	}

	return nil
}

func (k *KubeInfo) generateIstioCore(dst, module string) error {
	src := util.GetResourcePath(filepath.Join(istioInstallDir, fmt.Sprintf("istio-%s.yaml", module)))
	ori, err := ioutil.ReadFile(src)
	if err != nil {
		glog.Errorf("Cannot read original yaml file %s", src)
		return err
	}
	var image []byte
	switch module {
	case "manager":
		image = []byte(fmt.Sprintf("image: %s", k.ManagerImage))
	case "mixer":
		image = []byte(fmt.Sprintf("image: %s", k.MixerImage))
	case "ingress":
		image = []byte(fmt.Sprintf("image: %s", k.ProxyImage))
	}
	r := regexp.MustCompile(`image: .*(\/.*):.*`)
	content := r.ReplaceAllLiteral(ori, image)
	err = ioutil.WriteFile(dst, content, 0600)
	if err != nil {
		glog.Errorf("Cannot write into generated yaml file %s", dst)
	}
	return err
}

// deploysApps deploys all the apps registered
func (k *KubeInfo) deployApps() error {
	for _, a := range k.Apps {
		if err := a.Deploy(k.yamlDir, k.Namespace, k.Istioctl); err != nil {
			return err
		}
	}
	return nil
}

// AddApp for automated deployment. Must be done before Setup Call.
func (k *KubeInfo) AddApp(a AppInterface) {
	k.Apps = append(k.Apps, a)
}
