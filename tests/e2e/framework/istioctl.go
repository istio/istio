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

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

const (
	istioctlURL   = "ISTIOCTL_URL"
	proxyHubConst = "HUB"
	proxyTagConst = "TAG"
)

var (
	defaultProxy = flag.Bool("default_proxy", false, "Test with default proxy hub and tag")
	remotePath   = flag.String("istioctl_url", os.Getenv(istioctlURL), "URL to download istioctl")
	localPath    = flag.String("istioctl", "", "Use local istioctl instead of remote")
)

// Istioctl gathers istioctl information.
type Istioctl struct {
	localPath       string
	remotePath      string
	binaryPath      string
	namespace       string
	proxyHub        string
	proxyTag        string
	imagePullPolicy string
	yamlDir         string
	// If true, will ignore proxyHub and proxyTag but use the default one.
	defaultProxy bool
	// if non-null, used for sidecar inject (note: proxyHub and proxyTag overridden)
	injectConfigMap string
}

// NewIstioctl create a new istioctl by given temp dir.
func NewIstioctl(yamlDir, namespace, proxyHub, proxyTag, imagePullPolicy, injectConfigMap string) (*Istioctl, error) {
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
		localPath:       *localPath,
		remotePath:      *remotePath,
		binaryPath:      filepath.Join(tmpDir, "istioctl"),
		namespace:       namespace,
		proxyHub:        proxyHub,
		proxyTag:        proxyTag,
		imagePullPolicy: imagePullPolicy,
		yamlDir:         filepath.Join(yamlDir, "istioctl"),
		defaultProxy:    *defaultProxy,
		injectConfigMap: injectConfigMap,
	}, nil
}

// Setup set up istioctl prerequest for tests, port forwarding
func (i *Istioctl) Setup() error {
	log.Info("Setting up istioctl")
	if err := i.Install(); err != nil {
		log.Error("Failed to download istioctl")
		return err
	}
	return nil
}

// Teardown clean up everything created by setup
func (i *Istioctl) Teardown() error {
	log.Info("Cleaning up istioctl")
	return nil
}

// Install downloads Istioctl binary.
func (i *Istioctl) Install() error {
	if i.localPath == "" {
		if i.remotePath == "" {
			// If a remote URL or env variable is not set, default to the locally built istioctl
			gopath := os.Getenv("GOPATH")
			i.localPath = filepath.Join(gopath, "/bin/istioctl")
			i.binaryPath = i.localPath
			return nil
		}
		var usr, err = user.Current()
		if err != nil {
			log.Error("Failed to get current user")
			return err
		}
		homeDir := usr.HomeDir

		istioctlSuffix, err := util.GetOsExt()
		if err != nil {
			return err
		}
		if err = util.HTTPDownload(i.binaryPath, i.remotePath+"/istioctl-"+istioctlSuffix); err != nil {
			log.Error("Failed to download istioctl")
			return err
		}
		err = os.Chmod(i.binaryPath, 0755) // #nosec
		if err != nil {
			log.Error("Failed to add execute permission to istioctl")
			return err
		}
		i.binaryPath = fmt.Sprintf("%s -c %s/.kube/config", i.binaryPath, homeDir)
	} else {
		i.binaryPath = i.localPath
	}
	return nil
}

func (i *Istioctl) run(format string, args ...interface{}) error {
	format = i.binaryPath + " " + format
	if _, err := util.Shell(format, args...); err != nil {
		log.Errorf("istioctl %s failed", args)
		return err
	}
	return nil
}

// KubeInject use istio kube-inject to create new yaml with a proxy as sidecar.
// TODO The commands below could be generalized so that istioctl doesn't default to
// using the in cluster kubeconfig this is useful in multicluster cases to perform
// injection on remote clusters.
func (i *Istioctl) KubeInject(src, dest, kubeconfig string) error {
	injectCfgMapStr := ""
	if i.injectConfigMap != "" {
		injectCfgMapStr = fmt.Sprintf("--injectConfigMapName %s", i.injectConfigMap)
	}
	kubeconfigStr := ""
	if kubeconfig != "" {
		kubeconfigStr = " --kubeconfig " + kubeconfig
	}
	if i.defaultProxy {
		return i.run(`kube-inject -f %s -o %s -n %s -i %s --meshConfigMapName=istio %s %s`,
			src, dest, i.namespace, i.namespace, injectCfgMapStr, kubeconfigStr)
	}

	imagePullPolicyStr := ""
	if i.imagePullPolicy != "" {
		imagePullPolicyStr = fmt.Sprintf("--imagePullPolicy %s", i.imagePullPolicy)
	}
	hubAndTagStr := ""
	if i.injectConfigMap == "" {
		hubAndTagStr = fmt.Sprintf("--hub %s --tag %s", i.proxyHub, i.proxyTag)
	}
	return i.run(`kube-inject -f %s -o %s %s %s -n %s -i %s --meshConfigMapName=istio %s %s`,
		src, dest, hubAndTagStr, imagePullPolicyStr, i.namespace, i.namespace, injectCfgMapStr, kubeconfigStr)
}

// CreateRule create new rule(s)
func (i *Istioctl) CreateRule(rule string) error {
	return i.run("-n %s create -f %s", i.namespace, rule)
}

// ReplaceRule replace rule(s)
func (i *Istioctl) ReplaceRule(rule string) error {
	return i.run("-n %s replace -f %s", i.namespace, rule)
}

// DeleteRule Delete rule(s)
func (i *Istioctl) DeleteRule(rule string) error {
	return i.run("-n %s delete -f %s", i.namespace, rule)
}
