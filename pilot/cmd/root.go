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

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"istio.io/manager/cmd/version"
	"istio.io/manager/model"
	"istio.io/manager/platform/kube"
)

// Flags are CLI kubernetes client parameters
type Flags struct {
	Kubeconfig string
	Namespace  string
}

var (
	// RootFlags instance
	RootFlags = &Flags{}

	// Client is a kubernetes client interface set-up by the root command
	Client *kube.Client

	// RootCmd is the root CLI command
	RootCmd = &cobra.Command{
		Short: "Istio Manager",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
			glog.V(2).Infof("Root flags: %#v", RootFlags)
			Client, err = kube.NewClient(RootFlags.Kubeconfig, model.IstioConfig)
			if err != nil {
				Client, err = kube.NewClient(os.Getenv("HOME")+"/.kube/config", model.IstioConfig)
				if err != nil {
					return multierror.Prefix(err, "failed to connect to Kubernetes API.")
				}
			}
			if err = Client.RegisterResources(); err != nil {
				return multierror.Prefix(err, "failed to register Third-Party Resources.")
			}
			return
		},
	}
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Display version information and exit",
		RunE: func(*cobra.Command, []string) error {
			fmt.Printf("Version: %v\n", version.Info.Version)
			fmt.Printf("GitRevision: %v\n", version.Info.GitRevision)
			fmt.Printf("GitBranch: %v\n", version.Info.GitBranch)
			fmt.Printf("User: %v@%v\n", version.Info.User, version.Info.Host)
			fmt.Printf("GolangVersion: %v\n", version.Info.GolangVersion)
			return nil
		},
	}
)

func init() {
	RootCmd.PersistentFlags().StringVarP(&RootFlags.Kubeconfig, "kubeconfig", "c", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	RootCmd.PersistentFlags().StringVarP(&RootFlags.Namespace, "namespace", "n", "",
		"Select a Kubernetes namespace")

	// carry over glog flags with new defaults
	flag.CommandLine.VisitAll(func(gf *flag.Flag) {
		switch gf.Name {
		case "logtostderr":
			err := gf.Value.Set("true")
			if err != nil {
				fmt.Printf("missing logtostderr flag: %v", err)
			}
		case "alsologtostderr", "log_dir", "stderrthreshold":
			// always use stderr for logging
		default:
			RootCmd.PersistentFlags().AddGoFlag(gf)
		}
	})

	RootCmd.AddCommand(versionCmd)
}

// WaitSignal awaits for SIGINT or SIGTERM and closes the channel
func WaitSignal(stop chan struct{}) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	close(stop)
	glog.Flush()
}
