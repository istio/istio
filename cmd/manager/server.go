// Copyright 2019 Istio Authors
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

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	drm "github.com/openshift/cluster-network-operator/pkg/util/k8s"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"istio.io/operator/pkg/apis"
	"istio.io/operator/pkg/controller"
	"istio.io/operator/pkg/controller/istiocontrolplane"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
)

// Should match deploy/service.yaml
const (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8383
)

func serverCmd() *cobra.Command {
	loggingOptions := log.DefaultOptions()
	introspectionOptions := ctrlz.DefaultOptions()

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

			run()
			return nil
		},
	}

	loggingOptions.AttachCobraFlags(serverCmd)
	introspectionOptions.AttachCobraFlags(serverCmd)
	istiocontrolplane.AttachCobraFlags(serverCmd)

	return serverCmd
}

// getWatchNamespace returns the namespace the operator should be watching for changes
func getWatchNamespace() (string, error) {
	ns, found := os.LookupEnv("WATCH_NAMESPACE")
	if !found {
		return "", fmt.Errorf("WATCH_NAMESPACE must be set")
	}
	return ns, nil
}

// getLeaderElectionNamespace returns the namespace in which the leader election configmap will be created
func getLeaderElectionNamespace() (string, bool) {
	return os.LookupEnv("LEADER_ELECTION_NAMESPACE")
}

func run() {
	watchNS, err := getWatchNamespace()
	if err != nil {
		log.Fatalf("Failed to get watch namespace: %v", err)
	}

	leaderElectionNS, leaderElectionEnabled := getLeaderElectionNamespace()
	if !leaderElectionEnabled {
		log.Warn("Leader election namespace not set. Leader election is disabled. NOT APPROPRIATE FOR PRODUCTION USE!")
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatalf("Could not get apiserver config: %v", err)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          watchNS,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
		// Workaround for https://github.com/kubernetes-sigs/controller-runtime/issues/321
		MapperProvider:          drm.NewDynamicRESTMapper,
		LeaderElection:          leaderElectionEnabled,
		LeaderElectionNamespace: leaderElectionNS,
		LeaderElectionID:        "istio-operator-lock",
	})
	if err != nil {
		log.Fatalf("Could not create a controller manager: %v", err)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Fatalf("Could not add manager scheme: %v", err)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Fatalf("Could not add all controllers to operator manager: %v", err)
	}

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Fatalf("Manager exited non-zero: %v", err)
	}
}
