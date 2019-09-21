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
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"github.com/operator-framework/operator-sdk/pkg/leader"
	"github.com/operator-framework/operator-sdk/pkg/metrics"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"

	"istio.io/operator/pkg/apis"
	"istio.io/operator/pkg/controller"
	"istio.io/operator/pkg/controller/istiocontrolplane"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
)

// Change below variables to serve metrics on different host or port.
var (
	metricsHost              = "0.0.0.0"
	metricsPort        int32 = 8383
	metricsServicePort       = []v1.ServicePort{
		{Port: metricsPort, Name: metrics.OperatorPortName, Protocol: v1.ProtocolTCP, TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: metricsPort}},
	}
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

func run() {
	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		log.Fatalf("Failed to get watch namespace: %v", err)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatalf("Could not get apiserver config: %v", err)
	}

	ctx := context.TODO()

	// Become the leader before proceeding
	err = leader.Become(ctx, "istio-operator-lock")
	if err != nil {
		log.Fatalf("Could not become the leader: %v", err)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          namespace,
		MetricsBindAddress: fmt.Sprintf("%s:%d", metricsHost, metricsPort),
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

	// Create Service object to expose the metrics port.
	_, err = metrics.CreateMetricsService(ctx, cfg, metricsServicePort)
	if err != nil {
		log.Errorf("Could not create a service to expose the metrics port: %v", err.Error())
	}

	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Fatalf("Manager exited non-zero: %v", err)
	}
}
