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

package minikube

import (
	"fmt"
	"os"
	"strings"

	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
	"istio.io/istio/tests/util/localregistry"
)

const (
	// Default values for test env setup
	LocalRegistryFile      = "tests/util/localregistry/localregistry.yaml"
	LocalRegistryNamespace = "kube-system"
	LocalRegistryPort      = 5000
)

// setupMinikubeEnvironment starts minikube and sets up local registry in minikube's k8's cluster.
func SetupMinikubeEnvironment(cpus, remoteRegistryPort uint16, memory uint64, vmDriver, kubernetesVersion, kubeconfig string, insecureRegistry, extraOptions []string) error {
	goPath := os.Getenv("GOPATH")
	if len(goPath) == 0 {
		return fmt.Errorf("GOPATH not set.")
	}

	if err := startMinikube(cpus, memory, vmDriver, kubernetesVersion, insecureRegistry, extraOptions); err != nil {
		return fmt.Errorf("Minikube could not be started. err: %v", err)
	}
	if err := checkMinikubeRunning(); err != nil {
		return fmt.Errorf("Minikube not running. err: %v", err)
	}

	log.Infof("#Setting up in-cluster local registry in minikube kuberntes cluster#")
	if err := localregistry.SetupLocalRegistry(remoteRegistryPort, kubeconfig); err != nil {
		return fmt.Errorf("Local Registry Setup failed. err: %v", err)
	}
	log.Infof("")
	log.Infof("#Minikube Setup is done#")
	log.Infof("#Please export HUB=localhost:%d in your console#", remoteRegistryPort)

	return nil
}

func startMinikube(cpus uint16, memory uint64, vmDriver, kubernetesVersion string, insecureRegistry, extraOptions []string) error {
	log.Infof("#Starting Minikube#")
	startMinikubeCmd := fmt.Sprintf("minikube start --kubernetes-version=%s --cpus=%d --memory=%d "+
		"--vm-driver=%s --extra-config=%s --insecure-registry=%s", kubernetesVersion, cpus, memory, vmDriver,
		strings.Join(extraOptions, " --extra-config="), strings.Join(insecureRegistry, " --insecure-registry="))
	if _, err := util.Shell(startMinikubeCmd); err != nil {
		return fmt.Errorf("Failed starting minikube: %v", err)
	}

	return nil
}

func deleteMinikube() error {
	if _, err := util.Shell("minikube delete"); err != nil {
		return fmt.Errorf("Failed deleting minikube: %v", err)
	}

	return nil
}

func checkMinikubeRunning() error {
	count := 0
	checkPodCmd := fmt.Sprintf("kubectl get pods -n kube-system | grep kube-proxy |  grep Running")
	for count < 10 {
		// Wait for registry to be up.
		if _, err := util.Shell(checkPodCmd); err != nil {
			time.Sleep(5 * time.Second)
			count++
			continue
		}
		return nil
	}

	return fmt.Errorf("kube-proxy pod is not ready yet.")
}

// deleteMinikubeEnvironment deletes minikube, local registry and clean up any docker environment setup to talk to
// minikube.
func DeleteMinikubeEnvironment(kubeconfig string) error {
	if err := localregistry.TeardownLocalRegistry(kubeconfig); err != nil {
		log.Warnf("Failed to Cleanup LocalRegistry. err: %v", err)
	}
	if err := deleteMinikube(); err != nil {
		log.Errorf("Failed to Cleanup LocalRegistry. err: %v", err)
		return fmt.Errorf("Failed to Cleanup LocalRegistry. err: %v", err)
	}
	log.Infof("")
	log.Infof("#Minikube environment cleaned from the host machine#")

	return nil
}
