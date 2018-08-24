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

package main

import (
	"os"

	"fmt"

	"github.com/spf13/cobra"

	"istio.io/istio/tests/util/minikube"
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

func main() {
	rootCmd := GetRootCmd(os.Args[1:])

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "minikubeenv",
		Short: "minikubeenv is a tool to setup minikube environment for Istio Testing",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.AddCommand(setupCfgCmd(args))
	rootCmd.AddCommand(teardownCfgCmd(args))

	return rootCmd
}

func setupCfgCmd(rawArgs []string) *cobra.Command {
	sa := &minikubeSetupArgs{}
	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "sets up minikube environment for Istio testing",
		Run: func(cmd *cobra.Command, args []string) {
			if err := minikube.SetupMinikubeEnvironment(sa.cpus, sa.remoteRegistryPort, sa.memory, sa.vmDriver, sa.kubernetesVersion, sa.kubeconfig, sa.insecureRegistry, sa.extraOptions); err != nil {
				fmt.Println("Error Setting up minikube. err %v", err)
			} else {
				fmt.Println("Minikube is Setup.")
			}
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

func teardownCfgCmd(rawArgs []string) *cobra.Command {
	sa := &minikubeSetupArgs{}
	teardownCmd := &cobra.Command{
		Use:   "delete",
		Short: "deletes minikube environmnet",
		Run: func(cmd *cobra.Command, args []string) {
			if err := minikube.DeleteMinikubeEnvironment(sa.kubeconfig); err != nil {
				fmt.Println("Error Deleting Minikube. err %v", err)
			} else {
				fmt.Println("Minikube is deleted.")
			}
		},
	}
	teardownCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return teardownCmd
}
