// Copyright Istio Authors
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
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"go.opencensus.io/stats/view"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"istio.io/istio/mdp/controller/pkg/apis"
	mdpmanager "istio.io/istio/mdp/controller/pkg/manager"
	"istio.io/istio/mdp/controller/pkg/metrics"
	"istio.io/istio/mdp/controller/pkg/name"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

// Should match deploy/service.yaml
const (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 15014
)

func serverCmd() *cobra.Command {
	loggingOptions := log.DefaultOptions()
	introspectionOptions := ctrlz.DefaultOptions()

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Starts the MDP server",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := log.Configure(loggingOptions); err != nil {
				log.Errorf("Unable to configure logging: %v", err)
			}

			if cs, err := ctrlz.Run(introspectionOptions, nil); err == nil {
				defer cs.Close()
			} else {
				log.Errorf("Unable to initialize ControlZ: %v", err)
			}

			run()
			return nil
		},
	}

	loggingOptions.AttachCobraFlags(serverCmd)
	introspectionOptions.AttachCobraFlags(serverCmd)

	return serverCmd
}

func run() {
	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatalf("Could not get apiserver config: %v", err)
	}

	var mgrOpt manager.Options
	namespaces := strings.Split(name.MDPNamespace, ",")
	// Create MultiNamespacedCache with watched namespaces if it's not empty.
	mgrOpt = manager.Options{
		NewCache:           cache.MultiNamespacedCacheBuilder(namespaces),
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, mgrOpt)
	if err != nil {
		log.Fatalf("Could not create a controller manager: %v", err)
	}

	log.Info("Creating MDP metrics exporter")
	exporter, err := ocprom.NewExporter(ocprom.Options{
		Registry:  ctrlmetrics.Registry.(*prometheus.Registry),
		Namespace: "istio_mdp",
	})
	if err != nil {
		log.Warnf("Error while building exporter: %v", err)
	} else {
		view.RegisterExporter(exporter)
	}

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Fatalf("Could not add manager scheme: %v", err)
	}

	// Setup all Controllers
	if err := mdpmanager.AddToManager(mgr); err != nil {
		log.Fatalf("Could not add all controllers to operator manager: %v", err)
	}

	// Record version of MDP in metrics
	metrics.Version.
		With(metrics.MDPVersionLabel.Value(version.Info.String())).
		Record(1.0)

	log.Info("Starting the server.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Fatalf("Manager exited non-zero: %v", err)
	}
}
