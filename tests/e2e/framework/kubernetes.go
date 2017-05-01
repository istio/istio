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
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang/glog"

	"istio.io/istio/tests/e2e/util"
)

const (
	yamlSuffix       = ".yaml"
	yamlTmplDir      = "tests/e2e/framework/testdata/"
	mixerHubEnvVar   = "MIXER_HUB"
	mixerTagEnvVar   = "MIXER_TAG"
	managerHubEnvVar = "MANAGER_HUB"
	managerTagEnvVar = "MANAGER_TAG"
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

	Istioctl *util.Istioctl

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
		ProxyImage:       fmt.Sprintf("%s/proxy:%s", *managerHub, *managerTag),
		Verbosity:        verbosity,
		TmpDir:           tmpDir,
		yamlDir:          filepath.Join(tmpDir, "yaml"),
		Istioctl:         util.NewIstioctl(tmpDir, *namespace, *managerHub, *managerTag),
	}
}

// Setup set up Kubernetes prerequest for tests
func (k *KubeInfo) Setup() error {
	if err := os.Mkdir(k.yamlDir, os.ModeDir|os.ModePerm); err != nil {
		return err
	}
	if err := k.Istioctl.Install(); err != nil {
		return err
	}
	if err := util.CreateNamespace(k.Namespace); err != nil {
		glog.Error("Failed to create namespace.")
		return err
	}
	k.namespaceCreated = true
	if err := k.deployIstio(); err != nil {
		glog.Error("Failed to deployIstio.")
		return err
	}

	if err := k.deployApps(); err != nil {
		glog.Error("Failed to deploy apps")
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
	if k.namespaceCreated {
		if err := util.DeleteNamespace(k.Namespace); err != nil {
			return err
		}
		k.namespaceCreated = false
		glog.Infof("Namespace %s deleted", k.Namespace)
	}
	return nil
}

// deployIstio modules
func (k *KubeInfo) deployIstio() error {

	if err := k.deployIstioCore("istio-manager.yaml"); err != nil {
		return err
	}
	if err := k.deployIstioCore("istio-mixer.yaml"); err != nil {
		return err
	}
	return k.deployIstioCore("istio-ingress-controller.yaml")
}

// deployIstioCore deploy istio module from yaml files
func (k *KubeInfo) deployIstioCore(name string) error {
	yamlFile, err := util.CreateTempfile(k.yamlDir, name, yamlSuffix)
	tmpl := util.GetResourcePath(filepath.Join(yamlTmplDir, fmt.Sprintf("%s.tmpl", name)))
	if err != nil {
		return err
	}
	if err := util.Fill(yamlFile, tmpl, *k); err != nil {
		glog.Errorf("Failed to fill %s", yamlFile)
		return err
	}
	if err := util.KubeApply(k.Namespace, yamlFile); err != nil {
		glog.Errorf("Kubectl apply %s failed", yamlFile)
		return err
	}
	return nil
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
