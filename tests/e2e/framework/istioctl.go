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
	"os/user"
	"path/filepath"
	"runtime"

	"github.com/golang/glog"

	"istio.io/istio/tests/e2e/util"
)

const (
	istioctlURL = "ISTIOCTL_URL"
	// We use proxy always from pilot, at lease for now, so proxy and pilot always share the same hub and tag
	proxyHubConst = "PILOT_HUB"
	proxyTagConst = "PILOT_TAG"
)

var (
	remotePath = flag.String("istioctl_url", os.Getenv(istioctlURL), "URL to download istioctl")
	localPath  = flag.String("istioctl", "", "Use local istioctl instead of remote")
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
func NewIstioctl(yamlDir, namespace, istioNamespace, proxyHub, proxyTag string) (*Istioctl, error) {
	tmpDir, err := ioutil.TempDir(os.TempDir(), tmpPrefix)
	if err != nil {
		return nil, err
	}

	if proxyHub == "" {
		proxyHub = os.Getenv(proxyHubConst)
	}
	if proxyTag == "" {
		proxyTag = os.Getenv(proxyTagConst)
	}

	return &Istioctl{
		remotePath: *remotePath,
		binaryPath: filepath.Join(tmpDir, "istioctl"),
		namespace:  namespace,
		proxyHub:   proxyHub,
		proxyTag:   proxyTag,
		yamlDir:    filepath.Join(yamlDir, "istioctl"),
	}, nil
}

// Setup set up istioctl prerequest for tests, port forwarding
func (i *Istioctl) Setup() error {
	glog.Info("Setting up istioctl")
	if err := i.Install(); err != nil {
		glog.Error("Failed to download istioclt")
		return err
	}
	return nil
}

// Teardown clean up everything created by setup
func (i *Istioctl) Teardown() error {
	glog.Info("Cleaning up istioctl")
	return nil
}

// Install downloads Istioctl binary.
func (i *Istioctl) Install() error {
	if *localPath == "" {
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
		default:
			return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
		}

		if err = util.HTTPDownload(i.binaryPath, i.remotePath+"/istioctl-"+istioctlSuffix); err != nil {
			return err
		}
		err = os.Chmod(i.binaryPath, 0755) // #nosec
		if err != nil {
			return err
		}
		i.binaryPath = fmt.Sprintf("%s -c %s/.kube/config", i.binaryPath, homeDir)
	} else {
		i.binaryPath = *localPath
	}
	return nil
}

func (i *Istioctl) run(format string, args ...interface{}) error {
	format = i.binaryPath + " " + format
	if _, err := util.Shell(format, args...); err != nil {
		glog.Errorf("istioctl %s failed", args)
		return err
	}
	return nil
}

// KubeInject use istio kube-inject to create new yaml with a proxy as sidecar.
func (i *Istioctl) KubeInject(src, dest string) error {
	return i.run("kube-inject -f %s -o %s --hub %s --tag %s -n %s",
		src, dest, i.proxyHub, i.proxyTag, i.namespace)
}

// CreateRule create new rule(s)
func (i *Istioctl) CreateRule(rule string) error {
	return i.run("-n %s create -f %s", i.namespace, rule)
}

// CreateMixerRule create new rule(s)
func (i *Istioctl) CreateMixerRule(scope, subject, rule string) error {
	return i.run("-n %s mixer rule create %s %s -f %s",
		i.namespace, scope, subject, rule)
}

// ReplaceRule replace rule(s)
func (i *Istioctl) ReplaceRule(rule string) error {
	return i.run("-n %s replace -f %s", i.namespace, rule)
}

// DeleteRule Delete rule(s)
func (i *Istioctl) DeleteRule(rule string) error {
	return i.run("-n %s delete -f %s", i.namespace, rule)
}
