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
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

const (
	// Default values for local test env setup
	LocalRegistryFile      = "tests/util/localregistry/localregistry.yaml"
	LocalRegistryNamespace = "kube-system"
	LocalRegistryPort      = 5000
	RemoteRegistryPort     = 5000
)

// LocalRegistry provides in-cluster docker registry for test
type LocalRegistry struct {
	namespace          string
	istioctl           *Istioctl
	active             bool
	Kubeconfig         string
	PortForwardProcess *os.Process
	RegSvcIp           string
}

// GetLocalRegistry sets up localregistry and return th registry
func GetLocalRegistry(istioctl *Istioctl, kubeconfig string, skipSetup bool) *LocalRegistry {
	goPath := os.Getenv("GOPATH")
	if len(goPath) == 0 {
		log.Errorf("GOPATH not set.")
		return nil
	}

	localRegistryFilePath := path.Join(util.GetResourcePath(LocalRegistryFile))
	if err := util.KubeApply(LocalRegistryNamespace, localRegistryFilePath, kubeconfig); err != nil {
		log.Errorf("Kubectl apply %s failed", LocalRegistryFile)
		return nil
	}

	count := 0
	for count < 10 {
		// Wait for registry to be up.
		if _, err := util.Shell("kubectl get pods -n %s | grep kube-registry-v0 | grep -q Running", LocalRegistryNamespace); err != nil {
			time.Sleep(5 * time.Second)
			count++
			continue
		}

		var err error
		var portForwardPod string
		// Registry is up now, try to get the registry pod for port-forwarding
		if portForwardPod, err = util.Shell("kubectl get po -n %s | grep kube-registry-v0 | awk '{print $1;}'", LocalRegistryNamespace); err != nil {
			return nil
		}

		// Setup Port-Forwarding for local registry.
		var proc *os.Process
		portFwdCmd := fmt.Sprintf("kubectl port-forward %s %d:%d -n %s", strings.Trim(portForwardPod, "\n\r'"), LocalRegistryPort, RemoteRegistryPort, LocalRegistryNamespace)
		if proc, err = util.RunBackground(portFwdCmd); err != nil {
			log.Errorf("Failed to port forward: %s", err)
			return nil
		}

		// Setup docker to talk to local registry
		/*dockerInfoCommand := "docker info | grep localhost:5000"
		if _, err = util.Shell(dockerInfoCommand); err != nil {
			var osName string
			if osName, err = util.GetOsExt(); err != nil {
				log.Errorf("Failed to get OS: %s", err)
				return nil
			}
			if osName == "osx" {
				log.Infof("Please setup localhost:5000 in your docker daemon's insecure registry. Sleepin for 5 minutes to allow doing that.")
				time.Sleep(5 * time.Minute)
			} else {
				dockerSetupCommand := "sudo sed -i 's/ExecStart=\\/usr\\/bin\\/dockerd -H fd:\\/\\//ExecStart=\\/usr\\/bin\\/dockerd -H fd:\\/\\/ --insecure-registry localhost:5000/' /lib/systemd/system/docker.service"
				dockerSetupCommand += " && sudo systemctl daemon-reload"
				dockerSetupCommand += " && sudo systemctl restart docker"
				if _, err = util.Shell(dockerSetupCommand); err != nil {
					log.Errorf("Failed to setup docker daemon for insecure local registry: %s", err)
					return nil
				}
			}
		}*/

		var regSvcIp string
		getRegSvcIpCmd := fmt.Sprintf("kubectl get service kube-registry -n %s -o jsonpath='{.spec.clusterIP}'", LocalRegistryNamespace)
		if regSvcIp, err = util.Shell(getRegSvcIpCmd); err != nil {
			log.Errorf("Failed to get ip address of local registry: %s", err)
			return nil
		}

		// Make docker and make push, to push the images to local registry.
		if !skipSetup {
			var currWorkingDir string

			if currWorkingDir, err = os.Getwd(); err != nil {
				log.Errorf("Could not get current working directory while setting up local registry and istio images: %s", err)
				return nil
			}
			err = os.Chdir(path.Join(goPath, "src/istio.io/istio"))
			if err != nil {
				log.Errorf("Could not change directory to %s: %s", path.Join(goPath, "src/istio.io/istio"), err)
				return nil
			}

			if _, err = util.Shell("GOOS=linux make docker.push HUB=localhost:5000"); err != nil {
				log.Errorf("Failed to make push: %s", err)
				return nil
			}
			generateYamlCommand := fmt.Sprintf("make istioctl generate_yaml installgen HUB=%s:5000", regSvcIp)
			if _, err = util.Shell(generateYamlCommand); err != nil {
				log.Errorf("Failed to make istioctl generate_yaml: %s", err)
				return nil
			}
			err = os.Chdir(currWorkingDir)
			if err != nil {
				log.Errorf("Could not change back to working directory %s: %s", currWorkingDir, err)
				return nil
			}
		}

		return &LocalRegistry{
			namespace:          LocalRegistryNamespace,
			istioctl:           istioctl,
			Kubeconfig:         kubeconfig,
			PortForwardProcess: proc,
			RegSvcIp:           regSvcIp,
		}
	}

	return nil
}

// Setup implements the Cleanable interface
func (l *LocalRegistry) Setup() error {
	l.active = true
	return nil
}

// Teardown implements the Cleanable interface
func (l *LocalRegistry) Teardown() error {
	if err := util.KubeDelete(LocalRegistryNamespace, path.Join(util.GetResourcePath(LocalRegistryFile)), l.Kubeconfig); err != nil {
		log.Errorf("Kubectl delete %s failed", l.Kubeconfig)
		return err
	}
	l.active = false
	err := l.PortForwardProcess.Kill()
	if err != nil {
		log.Errorf("Failed to kill port-forward process, pid: %d", l.PortForwardProcess.Pid)
	}
	return nil
}
