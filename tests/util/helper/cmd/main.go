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
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"istio.io/istio/tests/util/registry"
	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/pkg/log"
	"io"
)

type localRegistrySetupArgs struct {
	localRegistryPort uint16
	kubeconfig        string
}

// closerRegister is a non thread safe struct to register io.Closers
type closerRegister struct {
	closers [] io.Closer
}

func (cr *closerRegister) register(closer io.Closer) {
	cr.closers = append(cr.closers, closer)
}

func (cr *closerRegister) stop() {
	if len(cr.closers) == 0 {
		return
	}
	fmt.Println("Hit CTRL-C to interrupt")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
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
		cr.stop()
		os.Exit(-1)
	}
	cr.stop()
}

func wait() {

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

func inClusterRegistry(rawArgs []string, fatalf shared.FormatFn, cr *closerRegister) *cobra.Command {
	sa := &localRegistrySetupArgs{}
	inClusterCmd := &cobra.Command{
		Use:   "in_cluster_registry",
		Short: "sets up a in-cluster registry in your Kubernetes cluster",
		Run: func(cmd *cobra.Command, args []string) {
			localReg, err := registry.NewInClusterRegistry(sa.kubeconfig, sa.localRegistryPort)
			if err != nil {
				fatalf("failed to instantiate an in-cluster registry. %v", err)
			}
			if err := localReg.Start(); err != nil {
				fatalf("error setting up in-cluster registry. err %v", err)
			} else {
				fmt.Printf("Local Registry is Setup at %s.\n", localReg.GetLocalRegistryAddress())
			}
			cr.register(localReg)
		},
	}
	inClusterCmd.PersistentFlags().Uint16Var(&sa.localRegistryPort, "localRegistryPort", 5000,
		"port number where registry can be port-forwarded on user's machine. Default is 5000, same as local "+
			"registry port.")
	inClusterCmd.PersistentFlags().StringVar(&sa.kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	return inClusterCmd
}
