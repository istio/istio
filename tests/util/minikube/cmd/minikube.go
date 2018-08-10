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
	"strings"

	"github.com/spf13/cobra"

	"os/exec"
	"time"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/tests/util"
	"istio.io/istio/tests/util/localregistry/cmd"
)

const (
	// Default values for test env setup
	LocalRegistryFile      = "tests/util/localregistry/localregistry.yaml"
	LocalRegistryNamespace = "kube-system"
	LocalRegistryPort      = 5000
)

type minikubeSetupArgs struct {
	extraOptions       []string
	kubernetesVersion  string
	insecureRegistry   []string
	cpus               uint16
	memory             uint64
	vmDriver           string
	remoteRegistryPort uint16
	kubeconfig         string
}

func setupCfgCmd(rawArgs []string, printf, fatalf shared.FormatFn) *cobra.Command {
	sa := &minikubeSetupArgs{}
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "sets up minikube environment for Istio testing",
		Run: func(cmd *cobra.Command, args []string) {
			setupMinikubeEnvironment(sa, printf, fatalf)
		},
	}

	setupCmd.PersistentFlags().StringSliceVar(&sa.extraOptions, "extra-config", []string{
		"controller-manager.cluster-signing-cert-file=\"/var/lib/localkube/certs/ca.crt\"",
		"controller-manager.cluster-signing-key-file=\"/var/lib/localkube/certs/ca.key\"",
		"apiserver.admission-control=\"NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel," +
			"DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota\""},
		`A set of key=value pairs that describe configuration that may be passed to different components.
		The key should be '.' separated, and the first part before the dot is the component to apply the configuration to.
		Valid components are: kubelet, apiserver, controller-manager, etcd, proxy, scheduler.`)
	setupCmd.PersistentFlags().Uint16Var(&sa.cpus, "cpus", 4, "Number of CPUs allocated to the "+
		"minikube VM")
	setupCmd.PersistentFlags().Uint64Var(&sa.memory, "memory", 8192, "Amount of RAM allocated to"+
		" the minikube VM in MB")
	setupCmd.PersistentFlags().StringVar(&sa.kubernetesVersion, "kubernetes-version", "v1.10.0",
		"Kubernetes version to use for setting up minikube")
	setupCmd.PersistentFlags().StringSliceVar(&sa.insecureRegistry, "insecure-registry", []string{"localhost:5000"},
		"Insecure Docker registries to pass to the Docker daemon.  The default service CIDR range will automatically"+
			" be added.")
	setupCmd.PersistentFlags().StringVar(&sa.vmDriver, "vm-driver", "none", "vm driver for minikube"+
		" in Istio. Supported are none, hyperkit and kvm2")
	setupCmd.PersistentFlags().Uint16Var(&sa.remoteRegistryPort, "remoteRegistryPort", 5000, "port"+
		" number where registry can be port-forwarded on user's machine. Default is 5000, same as local registry port.")
	setupCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return setupCmd
}

func teardownCfgCmd(rawArgs []string, printf, fatalf shared.FormatFn) *cobra.Command {
	sa := &minikubeSetupArgs{}
	teardownCmd := &cobra.Command{
		Use:   "delete",
		Short: "deletes minikube environmnet",
		Run: func(cmd *cobra.Command, args []string) {
			deleteMinikubeEnvironment(sa, printf, fatalf)
		},
	}
	teardownCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return teardownCmd
}

// setupMinikubeEnvironment starts minikube and sets up local registry in minikube's k8's cluster.
func setupMinikubeEnvironment(sa *minikubeSetupArgs, printf, fatalf shared.FormatFn) {
	goPath := os.Getenv("GOPATH")
	if len(goPath) == 0 {
		fatalf("GOPATH not set.")
	}

	startMinikube(sa, printf, fatalf)
	setupDockerToTalkToMinikube(printf, fatalf)
	checkMinikubeRunning(fatalf)

	printf("#Setting up in-cluster local registry in minikube kuberntes cluster#")
	cmd.SetupLocalRegistry(sa.remoteRegistryPort, sa.kubeconfig, printf, fatalf)
	printf("")
	printf("#Minikube Setup is done#")
	printf("#Please export HUB=localhost:%d in your console#", sa.remoteRegistryPort)
}

func startMinikube(sa *minikubeSetupArgs, printf, fatalf shared.FormatFn) {
	printf("#Starting Minikube#")
	startMinikubeCmd := fmt.Sprintf("minikube start --kubernetes-version=%s --cpus=%d --memory=%d "+
		"--vm-driver=%s --extra-config=%s --insecure-registry=%s", sa.kubernetesVersion, sa.cpus, sa.memory, sa.vmDriver,
		strings.Join(sa.extraOptions, " --extra-config="), strings.Join(sa.insecureRegistry, " --insecure-registry="))
	if _, err := util.Shell(startMinikubeCmd); err != nil {
		fatalf("Failed starting minikube: %v", err)
	}
}

func deleteMinikube(sa *minikubeSetupArgs, printf, fatalf shared.FormatFn) {
	if _, err := util.Shell("minikube delete"); err != nil {
		fatalf("Failed deleting minikube: %v", err)
	}
}

func setupDockerToTalkToMinikube(printf, fatalf shared.FormatFn) {
	printf("Running Command: eval $(minikube docker-env)")
	c := exec.Command("bash", "-c", "eval $(minikube docker-env)")
	if err := c.Run(); err != nil {
		fatalf("Failed setting up docker environment to talk to minikube: %v", err)
	}
}

func cleanupDockerToTalkToMinikube(printf, fatalf shared.FormatFn) {
	printf("Running Command: eval $(minikube docker-env -u)")
	c := exec.Command("bash", "-c", "eval $(minikube docker-env -u)")
	if err := c.Run(); err != nil {
		fatalf("Failed setting up docker environment to talk to minikube: %v", err)
	}
}

func checkMinikubeRunning(fatalf shared.FormatFn) {
	count := 0
	checkPodCmd := fmt.Sprintf("kubectl get pods -n kube-system | grep kube-proxy |  grep Running")
	for count < 10 {
		// Wait for registry to be up.
		if _, err := util.Shell(checkPodCmd); err != nil {
			time.Sleep(5 * time.Second)
			count++
			continue
		}
		return
	}

	fatalf("kube-proxy pod is not ready yet.")
}

// deleteMinikubeEnvironment deletes minikube, local registry and clean up any docker environment setup to talk to
// minikube.
func deleteMinikubeEnvironment(sa *minikubeSetupArgs, printf, fatalf shared.FormatFn) {
	cmd.TeardownLocalRegistry(sa.kubeconfig, printf, fatalf)
	cleanupDockerToTalkToMinikube(printf, fatalf)
	deleteMinikube(sa, printf, fatalf)
	printf("")
	printf("#Minikube environment cleaned from the host machine#")
}
