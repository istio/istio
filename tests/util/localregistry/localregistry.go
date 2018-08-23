// Copyright 2018 Istio Authors
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

package localregistry

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"istio.io/istio/tests/util"
)

const (
	// Default values for test env setup
	LocalRegistryFile      = "tests/util/localregistry/localregistry.yaml"
	LocalRegistryNamespace = "kube-system"
	LocalRegistryPort      = 5000
)

// SetupLocalRegistry sets up localregistry and returns if any error was found while doing that.
func SetupLocalRegistry(remoteRegistryPort uint16, kubeconfig string) error {
	goPath := os.Getenv("GOPATH")
	if len(goPath) == 0 {
		return fmt.Errorf("GOPATH not set.")
	}

	localRegistryFilePath := path.Join(util.GetResourcePath(LocalRegistryFile))
	if err := util.KubeApply(LocalRegistryNamespace, localRegistryFilePath, kubeconfig); err != nil {
		return fmt.Errorf("Kubectl apply %s failed", LocalRegistryFile)
	}

	if err := checkLocalRegistryRunning(); err != nil {
		return fmt.Errorf("local registry not running. err:%v", err)
	}

	var err error
	var portForwardPod string
	// Registry is up now, try to get the registry pod for port-forwarding
	if portForwardPod, err = util.Shell("kubectl get po -n %s | grep kube-registry-v0 | awk '{print $1;}'",
		LocalRegistryNamespace); err != nil {
		TeardownLocalRegistry(kubeconfig)
		return fmt.Errorf("Could not get registry pod for port-forwarding: %v", err)
	}

	// Setup Port-Forwarding for local registry.
	portFwdCmd := fmt.Sprintf("kubectl port-forward %s %d:%d -n %s", strings.Trim(portForwardPod,
		"\n\r'"), LocalRegistryPort, remoteRegistryPort, LocalRegistryNamespace)
	if _, err = util.RunBackground(portFwdCmd); err != nil {
		TeardownLocalRegistry(kubeconfig)
		return fmt.Errorf("Failed to port forward: %v", err)
	}

	return nil
}

func checkLocalRegistryRunning() error {
	count := 0
	checkPodCmd := fmt.Sprintf("kubectl get pods -n kube-system | grep kube-registry-v0 | grep Running")
	for count < 10 {
		// Wait for registry to be up.
		if _, err := util.Shell(checkPodCmd); err != nil {
			time.Sleep(5 * time.Second)
			count++
			continue
		}
		return nil
	}

	return fmt.Errorf("kube-registry pod is not ready yet.")
}

// TeardownLocalRegistry deletes local registry from k8s cluster and cleans-up port forward processes too.
func TeardownLocalRegistry(kubeconfig string) error {
	if err := util.KubeDelete(LocalRegistryNamespace, path.Join(util.GetResourcePath(LocalRegistryFile)), kubeconfig); err != nil {
		return fmt.Errorf("Kubectl delete %s failed: %v", LocalRegistryFile, err)
	}
	_, err := util.Shell("killall kubectl")
	if err != nil {
		return fmt.Errorf("Failed to kill port-forward process: %v", err)
	}

	return nil
}
