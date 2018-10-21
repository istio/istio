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
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/docker/registry"
	testKube "istio.io/istio/pkg/test/kube"
)

type localRegistrySetupArgs struct {
	localRegistryPort uint16
	kubeconfig        string
	namespace         string
}

// closerRegister is a non thread safe struct to register io.Closers
type closerRegister struct {
	closers []io.Closer
}

func (cr *closerRegister) register(closer io.Closer) {
	cr.closers = append(cr.closers, closer)
}

func (cr *closerRegister) isUsed() bool {
	return len(cr.closers) > 0
}

func (cr *closerRegister) close() {
	if len(cr.closers) == 0 {
		return
	}
	for _, closer := range cr.closers {
		if err := closer.Close(); err != nil {
			log.Warna(err)
		}
	}
}

func main() {
	cr := &closerRegister{}
	rootCmd := getRootCmd(os.Args[1:], log.Fatalf, cr)
	if err := rootCmd.Execute(); err != nil {
		cr.close()
		os.Exit(-1)
	}
	if cr.isUsed() {
		fmt.Println("Hit CTRL-C to interrupt")
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		fmt.Println("Starting Tear Down")
	}
	cr.close()
}

// getRootCmd returns the root of the cobra command-tree.
func getRootCmd(args []string, fatalf shared.FormatFn, cr *closerRegister) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "helper",
		Short: "helper is a tool to help local development",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
	}
	rootCmd.SetArgs(args)
	rootCmd.AddCommand(inClusterRegistry(args, fatalf, cr))

	return rootCmd
}

func inClusterRegistry(_ []string, fatalf shared.FormatFn, cr *closerRegister) *cobra.Command {
	sa := &localRegistrySetupArgs{}
	inClusterCmd := &cobra.Command{
		Use:   "in_cluster_registry",
		Short: "sets up a in-cluster registry in your Kubernetes cluster",
		Run: func(cmd *cobra.Command, args []string) {
			accessor, err := testKube.NewAccessor(sa.kubeconfig)
			if err != nil {
				fatalf("failed to create accessor. %v", err)
			}
			localReg, err := registry.NewInClusterRegistry(accessor, sa.localRegistryPort, sa.namespace)
			if err != nil {
				fatalf("failed to instantiate an in-cluster registry. %v", err)
			}
			if err := localReg.Start(); err != nil {
				fatalf("error setting up in-cluster registry. err %v", err)
			} else {
				fmt.Printf("Local Registry is Setup at %s.\n", localReg.Address())
			}
			cr.register(localReg)
		},
	}
	inClusterCmd.PersistentFlags().Uint16Var(&sa.localRegistryPort, "port", 5000,
		"port number where registry can be port-forwarded on user's machine. Default is 5000, same as local "+
			"registry port.")
	inClusterCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	inClusterCmd.PersistentFlags().StringVar(&sa.namespace, "namespace", "docker-registry",
		"Update the namespace where the registry should be deployed")
	return inClusterCmd
}
