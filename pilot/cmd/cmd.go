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
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/model"
	"istio.io/pilot/proxy"
)

const (
	// ConfigMapKey is the key for mesh configuration data in the config map
	ConfigMapKey = "mesh"

	// DefaultConfigMapName is the default config map name that holds the mesh configuration.
	DefaultConfigMapName = "istio"
)

// GetMeshConfig fetches configuration from a config map
func GetMeshConfig(kube kubernetes.Interface, namespace, name string) (*proxyconfig.ProxyMeshConfig, error) {
	config, err := kube.CoreV1().ConfigMaps(namespace).Get(name, v1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// values in the data are strings, while proto might use a different data type.
	// therefore, we have to get a value by a key
	yaml, exists := config.Data[ConfigMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", ConfigMapKey)
	}

	mesh := proxy.DefaultMeshConfig()
	if err = model.ApplyYAML(yaml, &mesh); err != nil {
		return nil, multierror.Prefix(err, "failed to convert to proto.")
	}

	if err = model.ValidateProxyMeshConfig(&mesh); err != nil {
		return nil, err
	}

	return &mesh, nil
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
