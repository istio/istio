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
	"os"
	"strings"
	"time"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"go.opencensus.io/stats/view"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	root "istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/controller"
	"istio.io/istio/operator/pkg/controller/istiocontrolplane"
	"istio.io/istio/operator/pkg/metrics"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

// Should match deploy/service.yaml
const (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)

type serverArgs struct {
	// force proceeds even if there are validation errors
	force bool
}

func addServerFlags(cmd *cobra.Command, args *serverArgs) {
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, root.ForceFlagHelpStr)
}

func serverCmd() *cobra.Command {
	loggingOptions := log.DefaultOptions()
	introspectionOptions := ctrlz.DefaultOptions()
	sArgs := &serverArgs{}
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Starts the Istio operator server",
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

			run(sArgs)
			return nil
		},
	}

	loggingOptions.AttachCobraFlags(serverCmd)
	introspectionOptions.AttachCobraFlags(serverCmd)
	addServerFlags(serverCmd, sArgs)

	return serverCmd
}

// getWatchNamespaces returns the namespaces the operator should be watching for changes
func getWatchNamespaces() ([]string, error) {
	value, found := os.LookupEnv("WATCH_NAMESPACE")
	if !found {
		return nil, fmt.Errorf("WATCH_NAMESPACE must be set")
	}
	if value == "" {
		return nil, nil
	}
	return strings.Split(value, ","), nil
}

// getLeaderElectionNamespace returns the namespace in which the leader election configmap will be created
func getLeaderElectionNamespace() (string, bool) {
	return os.LookupEnv("LEADER_ELECTION_NAMESPACE")
}

// getRenewDeadline returns the renew deadline for active control plane to refresh leadership.
func getRenewDeadline() *time.Duration {
	ddl, found := os.LookupEnv("RENEW_DEADLINE")
	df := time.Second * 10
	if !found {
		return &df
	}
	duration, err := time.ParseDuration(ddl)
	if err != nil {
		log.Errorf("Failed to parse renewDeadline: %v, use default value: %s", err, df.String())
		return &df
	}
	return &duration
}

func run(sArgs *serverArgs) {
	watchNamespaces, err := getWatchNamespaces()
	if err != nil {
		log.Fatalf("Failed to get watch namespaces: %v", err)
	}

	leaderElectionNS, leaderElectionEnabled := getLeaderElectionNamespace()
	if !leaderElectionEnabled {
		log.Warn("Leader election namespace not set. Leader election is disabled. NOT APPROPRIATE FOR PRODUCTION USE!")
	}

	// renewDeadline cannot be greater than leaseDuration
	renewDeadline := getRenewDeadline()
	leaseDuration := time.Duration(renewDeadline.Nanoseconds() * 2)

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatalf("Could not get apiserver config: %v", err)
	}

	var mgrOpt manager.Options
	leaderElectionID := "istio-operator-lock"
	if operatorRevision, found := os.LookupEnv("REVISION"); found && operatorRevision != "" {
		leaderElectionID += "-" + operatorRevision
	}
	log.Infof("Leader election cm: %s", leaderElectionID)
	if len(watchNamespaces) > 0 {
		// Create MultiNamespacedCache with watched namespaces if it's not empty.
		mgrOpt = manager.Options{
			NewCache:                cache.MultiNamespacedCacheBuilder(watchNamespaces),
			MetricsBindAddress:      fmt.Sprintf("%s:%d", metricsHost, metricsPort),
			LeaderElection:          leaderElectionEnabled,
			LeaderElectionNamespace: leaderElectionNS,
			LeaderElectionID:        leaderElectionID,
			LeaseDuration:           &leaseDuration,
			RenewDeadline:           renewDeadline,
		}
	} else {
		// Create manager option for watching all namespaces.
		mgrOpt = manager.Options{
			Namespace:               "",
			MetricsBindAddress:      fmt.Sprintf("%s:%d", metricsHost, metricsPort),
			LeaderElection:          leaderElectionEnabled,
			LeaderElectionNamespace: leaderElectionNS,
			LeaderElectionID:        leaderElectionID,
			LeaseDuration:           &leaseDuration,
			RenewDeadline:           renewDeadline,
		}
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, mgrOpt)
	if err != nil {
		log.Fatalf("Could not create a controller manager: %v", err)
	}

	log.Info("Creating operator metrics exporter")
	exporter, err := ocprom.NewExporter(ocprom.Options{
		Registry:  ctrlmetrics.Registry.(*prometheus.Registry),
		Namespace: "istio_install_operator",
	})
	if err != nil {
		log.Warnf("Error while building exporter: %v", err)
	} else {
		view.RegisterExporter(exporter)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Fatalf("Could not add manager scheme: %v", err)
	}

	// Setup all Controllers
	options := &istiocontrolplane.Options{Force: sArgs.force}
	if err := controller.AddToManager(mgr, options); err != nil {
		log.Fatalf("Could not add all controllers to operator manager: %v", err)
	}

	// Record version of operator in metrics
	metrics.Version.
		With(metrics.OperatorVersionLabel.Value(version.Info.String())).
		Record(1.0)

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Fatalf("Manager exited non-zero: %v", err)
	}
}
