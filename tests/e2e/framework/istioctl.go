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
	"strings"

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
	kubeconfig      string
	yamlDir         string
	// If true, will ignore proxyHub and proxyTag but use the default one.
	defaultProxy bool
	// if non-null, used for sidecar inject (note: proxyHub and proxyTag overridden)
	injectConfigMap string
}

// NewIstioctl create a new istioctl by given temp dir.
func NewIstioctl(yamlDir, namespace, proxyHub, proxyTag, imagePullPolicy, injectConfigMap, kubeconfig string) (*Istioctl, error) {
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
		kubeconfig:      kubeconfig,
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

func (i *Istioctl) run(format string, args ...interface{}) (res string, err error) {
	format = i.binaryPath + " " + format
	if res, err = util.ShellMuteOutput(format, args...); err != nil {
		log.Errorf("istioctl %s failed", args)
		return "", err
	}
	return res, nil
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
		_, err := i.run(`kube-inject -f %s -o %s -n %s -i %s --meshConfigMapName=istio %s %s`,
			src, dest, i.namespace, i.namespace, injectCfgMapStr, kubeconfigStr)
		return err
	}

	imagePullPolicyStr := ""
	if i.imagePullPolicy != "" {
		imagePullPolicyStr = fmt.Sprintf("--imagePullPolicy %s", i.imagePullPolicy)
	}

	_, err := i.run(`kube-inject -f %s -o %s %s -n %s -i %s --meshConfigMapName=istio %s %s`,
		src, dest, imagePullPolicyStr, i.namespace, i.namespace, injectCfgMapStr, kubeconfigStr)
	return err
}

// DeleteRule Delete rule(s)
func (i *Istioctl) DeleteRule(rule string) error {
	_, err := i.run("-n %s delete -f %s", i.namespace, rule)
	return err
}

// PortEPs is a map from port number to a list of endpoints
type PortEPs map[string][]string

// GetProxyConfigEndpoints returns endpoints in the proxy config from a pod.
func (i *Istioctl) GetProxyConfigEndpoints(podName string, services []string) (epInfo map[string]PortEPs, err error) {
	// results have two columns: the first column indicates endpoint IPs and
	// the second column indicates envoy cluster names
	res, err := i.run("--kubeconfig=%s -n %s proxy-config endpoint %s", i.kubeconfig, i.namespace, podName)
	if err != nil {
		log.Errorf("Failed to get proxy-config endpoint from pod %s in namespace %s", podName, i.namespace)
		return nil, err
	}

	epInfo = make(map[string]PortEPs)
	for _, line := range strings.Split(res, "\n") {
		// fields[0] is the endpoint IP:port, fields[1] is the envoy cluster name
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			// Cluster name format direction|port|subsetName|hostname
			clusterNameFields := strings.Split(fields[2], "|")
			if len(clusterNameFields) != 4 {
				// static clusters don't follow the format
				continue
			}
			if clusterNameFields[0] == "outbound" {
				port := clusterNameFields[1]
				hostname := clusterNameFields[3]
				// hostname is a service FQDN with the format: service.namespace.svc.cluster.local
				// Only return services in the test namespace
				hostnameFields := strings.Split(hostname, ".")
				if hostnameFields[1] == i.namespace {
					appName := hostnameFields[0]
					if !func() bool {
						for _, svc := range services {
							if appName == svc {
								return true
							}
						}
						return false
					}() {
						continue
					}
					var portEps PortEPs
					if epInfo[appName] == nil {
						portEps = make(PortEPs)
						epInfo[appName] = portEps
					} else {
						portEps = epInfo[appName]
					}
					portEps[port] = append(portEps[port], strings.Split(fields[0], ":")[0])
				}
			}
		}
	}
	return epInfo, nil
}
