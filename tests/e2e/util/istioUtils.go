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

package util

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/golang/glog"
)

const (
	istioctlURL = "ISTIOCTL_URL"
)

var (
	remotePath = flag.String("istioctl_url", os.Getenv(istioctlURL), "URL to download istioctl")
)

// Istioctl gathers istioctl information.
type Istioctl struct {
	remotePath string
	binaryPath string
	namespace  string
	proxyHub   string
	proxyTag   string
	yamlDir    string
}

// NewIstioctl create a new istioctl by given temp dir.
func NewIstioctl(tmpDir, namespace, proxyHub, proxyTag string) *Istioctl {
	return &Istioctl{
		remotePath: *remotePath,
		binaryPath: filepath.Join(tmpDir, "/istioctl"),
		namespace:  namespace,
		proxyHub:   proxyHub,
		proxyTag:   proxyTag,
		yamlDir:    filepath.Join(tmpDir, "/istioctl"),
	}
}

// Install downloads Istioctl binary.
func (i *Istioctl) Install() error {
	var usr, err = user.Current()
	if err != nil {
		return err
	}
	homeDir := usr.HomeDir

	var istioctlSuffix string
	switch runtime.GOOS {
	case "linux":
		istioctlSuffix = "linux"
	case "darwin":
		istioctlSuffix = "osx"
	}

	if err = HTTPDownload(i.binaryPath, i.remotePath+"/istioctl-"+istioctlSuffix); err != nil {
		return err
	}
	err = os.Chmod(i.binaryPath, 0755) // #nosec
	if err != nil {
		return err
	}
	i.binaryPath = fmt.Sprintf("%s -c %s/.kube/config", i.binaryPath, homeDir)
	return nil
}

func (i *Istioctl) run(args string) error {
	if _, err := Shell(fmt.Sprintf("%s %s", i.binaryPath, args)); err != nil {
		glog.Errorf("istioctl %s failed", args)
		return err
	}
	return nil
}

// KubeInject use istio kube-inject to create new yaml with a proxy as sidecar.
func (i *Istioctl) KubeInject(src, dest string) error {
	args := fmt.Sprintf("kube-inject -f %s -o %s --hub %s --tag %s -n %s",
		src, dest, i.proxyHub, i.proxyTag, i.namespace)
	return i.run(args)
}

// CreateRule create new rule(s)
func (i *Istioctl) CreateRule(rule string) error {
	return i.run(fmt.Sprintf("-n %s create -f %s", i.namespace, rule))
}

// ReplaceRule replace rule(s)
func (i *Istioctl) ReplaceRule(rule string) error {
	return i.run(fmt.Sprintf("-n %s replace -f %s", i.namespace, rule))
}

// DeleteRule Delete rule(s)
func (i *Istioctl) DeleteRule(rule string) error {
	return i.run(fmt.Sprintf("-n %s delete -f %s", i.namespace, rule))
}
