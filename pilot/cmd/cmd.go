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
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
	"istio.io/pilot/tools/version"
)

// ReadMeshConfig gets mesh configuration from a config file
func ReadMeshConfig(filename string) (*proxyconfig.MeshConfig, error) {
	yaml, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, multierror.Prefix(err, "cannot read mesh config file")
	}

	mesh := proxy.DefaultMeshConfig()
	if err = model.ApplyYAML(string(yaml), &mesh); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	if err = model.ValidateMeshConfig(&mesh); err != nil {
		return nil, err
	}

	return &mesh, nil
}

// VersionCmd is a sub-command to print version information
var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display version information and exit",
	Run: func(*cobra.Command, []string) {
		fmt.Print(version.Version())
	},
}

// AddFlags carries over glog flags with new defaults
func AddFlags(rootCmd *cobra.Command) {
	flag.CommandLine.VisitAll(func(gf *flag.Flag) {
		switch gf.Name {
		case "logtostderr":
			if err := gf.Value.Set("true"); err != nil {
				fmt.Printf("missing logtostderr flag: %v", err)
			}
		case "alsologtostderr", "log_dir", "stderrthreshold":
			// always use stderr for logging
		default:
			rootCmd.PersistentFlags().AddGoFlag(gf)
		}
	})
}

// WaitSignal awaits for SIGINT or SIGTERM and closes the channel
func WaitSignal(stop chan struct{}) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
	close(stop)
	glog.Flush()
}
