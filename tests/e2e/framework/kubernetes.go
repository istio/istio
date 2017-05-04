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
	namespace    = flag.String("namespace", "", "Namespace to use for testing (empty to create/delete temporary one)")
	mixerHub     = flag.String("mixer_hub", os.Getenv(mixerHubEnvVar), "Mixer hub")
	mixerTag     = flag.String("mixer_tag", os.Getenv(mixerTagEnvVar), "Mixer tag")
	managerHub   = flag.String("manager_hub", os.Getenv(managerHubEnvVar), "Manager hub")
	managerTag   = flag.String("manager_tag", os.Getenv(managerTagEnvVar), "Manager tag")
	caHub        = flag.String("ca_hub", "", "Ca hub")
	caTag        = flag.String("ca_tag", "", "Ca tag")
	localCluster = flag.Bool("use_local_cluster", false, "Whether the cluster is local or not")

	modules = []string{
		"manager",
		"mixer",
		"ingress",
	}
)

// KubeInfo gathers information for kubectl
type KubeInfo struct {
	Namespace string

	MixerImage   string
	ManagerImage string
	CaImage      string
	ProxyImage   string

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
	i, err := NewIstioctl(yamlDir, *namespace, *managerHub, *managerTag)
	if err != nil {
		return nil, err
	}
	a := NewAppManager(tmpDir, *namespace, i)

	return &KubeInfo{
		Namespace:        *namespace,
		namespaceCreated: false,
		MixerImage:       fmt.Sprintf("%s/mixer:%s", *mixerHub, *mixerTag),
		ManagerImage:     fmt.Sprintf("%s/manager:%s", *managerHub, *managerTag),
		CaImage:          fmt.Sprintf("%s/ca:%s", *caHub, *caTag),
		// Proxy and Manager are released together and share the same hub and tag.
		ProxyImage:   fmt.Sprintf("%s/proxy:%s", *managerHub, *managerTag),
		TmpDir:       tmpDir,
		yamlDir:      yamlDir,
		localCluster: *localCluster,
		Istioctl:     i,
		AppManager:   a,
	}, nil
}

// Setup set up Kubernetes prerequest for tests
func (k *KubeInfo) Setup() error {
	glog.Info("Setting up kubeInfo")
	var err error
	if err = os.Mkdir(k.yamlDir, os.ModeDir|os.ModePerm); err != nil {
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
