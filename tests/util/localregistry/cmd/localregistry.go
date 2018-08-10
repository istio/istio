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

package cmd

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/tests/util"
)

const (
	// Default values for test env setup
	LocalRegistryFile      = "tests/util/localregistry/localregistry.yaml"
	LocalRegistryNamespace = "kube-system"
	LocalRegistryPort      = 5000
)

type localRegistrySetupArgs struct {
	remoteRegistryPort uint16
	kubeconfig         string
}

func setupCfgCmd(rawArgs []string, printf, fatalf shared.FormatFn) *cobra.Command {
	sa := &localRegistrySetupArgs{}
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "sets up localregistry in kubernetes cluster",
		Run: func(cmd *cobra.Command, args []string) {
			SetupLocalRegistry(sa.remoteRegistryPort, sa.kubeconfig, printf, fatalf)
		},
	}
	setupCmd.PersistentFlags().Uint16Var(&sa.remoteRegistryPort, "remoteRegistryPort", 5000,
		"port number where registry can be port-forwarded on user's machine. Default is 5000, same as local "+
			"registry port.")
	setupCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return setupCmd
}

func teardownCfgCmd(rawArgs []string, printf, fatalf shared.FormatFn) *cobra.Command {
	sa := &localRegistrySetupArgs{}
	teardownCmd := &cobra.Command{
		Use:   "delete",
		Short: "deletes localregistry in kubernetes cluster",
		Run: func(cmd *cobra.Command, args []string) {
			TeardownLocalRegistry(sa.kubeconfig, printf, fatalf)
		},
	}
	teardownCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return teardownCmd
}

// GetLocalRegistry sets up localregistry and return th registry
func SetupLocalRegistry(remoteRegistryPort uint16, kubeconfig string, printf, fatalf shared.FormatFn) {
	goPath := os.Getenv("GOPATH")
	if len(goPath) == 0 {
		fatalf("GOPATH not set.")
	}

	localRegistryFilePath := path.Join(util.GetResourcePath(LocalRegistryFile))
	if err := util.KubeApply(LocalRegistryNamespace, localRegistryFilePath, kubeconfig); err != nil {
		fatalf("Kubectl apply %s failed", LocalRegistryFile)
	}

	checkLocalRegistryRunning(fatalf)

	var err error
	var portForwardPod string
	// Registry is up now, try to get the registry pod for port-forwarding
	if portForwardPod, err = util.Shell("kubectl get po -n %s | grep kube-registry-v0 | awk '{print $1;}'",
		LocalRegistryNamespace); err != nil {
		fatalf("Could not get registry pod for port-forwarding: %v", err)
	}

	// Setup Port-Forwarding for local registry.
	portFwdCmd := fmt.Sprintf("kubectl port-forward %s %d:%d -n %s", strings.Trim(portForwardPod,
		"\n\r'"), LocalRegistryPort, remoteRegistryPort, LocalRegistryNamespace)
	if _, err = util.RunBackground(portFwdCmd); err != nil {
		fatalf("Failed to port forward: %v", err)
	}
}

func checkLocalRegistryRunning(fatalf shared.FormatFn) {
	count := 0
	checkPodCmd := fmt.Sprintf("kubectl get pods -n kube-system | grep kube-registry-v0 | grep Running")
	for count < 10 {
		// Wait for registry to be up.
		if _, err := util.Shell(checkPodCmd); err != nil {
			time.Sleep(5 * time.Second)
			count++
			continue
		}
		return
	}

	fatalf("kube-registry pod is not ready yet.")
}

// TeardownLocalRegistry deletes local registry from k8s cluster and cleans-up port forward processes too.
func TeardownLocalRegistry(kubeconfig string, printf, fatalf shared.FormatFn) {
	if err := util.KubeDelete(LocalRegistryNamespace, path.Join(util.GetResourcePath(LocalRegistryFile)), kubeconfig); err != nil {
		fatalf("Kubectl delete %s failed: %v", LocalRegistryFile, err)
	}
	_, err := util.Shell("killall kubectl")
	if err != nil {
		fatalf("Failed to kill port-forward process: %v", err)
	}
}
