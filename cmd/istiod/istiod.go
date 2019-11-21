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
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/serviceregistry"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
)

// Istio control plane with K8S support - minimal version.
//
// This uses the same code as Pilot, but restricts the config options to
// what is available in mesh.yaml (mesh config), so there should be no
// dependency on Helm, complex helm charts. This should run on a VM or
// in a docker container outside any k8s cluster, as long as the config
// files are mounted in the expected locations.
//
// - KUBECONFIG should point to a valid configuration, if not set in-cluster is used
// - optional /var/lib/istio/config/mesh.yaml will be loaded. Defaults should work for common cases.
// - local endpoints and additional multicluster registries loaded from K8S
// - additional MCP registries supported, based on mesh.yaml
// - certificates signed by K8S Apiserver.
//
func main() {
	stop := make(<-chan struct{})

	basePort := 15000

	// Create a test pilot discovery service configured to watch the tempDir.
	args := &bootstrap.PilotArgs{
		Config:       bootstrap.ConfigArgs{
			ControllerOptions: kubecontroller.Options{
				DomainSuffix: "cluster.local",
				TrustDomain: "cluster.local",
			},
		},
		Service: bootstrap.ServiceArgs{
			Registries: []string{string(serviceregistry.KubernetesRegistry)},
		},
		InjectionOptions: bootstrap.InjectionOptions {
			InjectionDirectory: "./var/lib/istio/inject",
			Port:               15017,
		},

		Plugins: bootstrap.DefaultPlugins, // TODO: Should it be in MeshConfig ? Env override until it's done.

		// MCP is messing up with the grpc settings...
		MCPMaxMessageSize:        1024 * 1024 * 64,
		MCPInitialWindowSize:     1024 * 1024 * 64,
		MCPInitialConnWindowSize: 1024 * 1024 * 64,
		BasePort: basePort,
	}

	// If the namespace isn't set, try looking it up from the environment.
	if args.Namespace == "" {
		args.Namespace = bootstrap.PodNamespaceVar.Get()
	}
	if args.KeepaliveOptions == nil {
		args.KeepaliveOptions = istiokeepalive.DefaultOption()
	}
	if args.Config.ClusterRegistriesNamespace == "" {
		args.Config.ClusterRegistriesNamespace = args.Namespace
	}
	args.DiscoveryOptions = bootstrap.DiscoveryServiceOptions{
		HTTPAddr: ":8080", // lots of tools use this
		GrpcAddr: fmt.Sprintf(":%d", basePort+10),
		// Using 12 for K8S-DNS based cert.
		// TODO: We'll also need 11 for Citadel-based cert
		SecureGrpcAddr:  "",
		EnableProfiling: true,
	}
	args.CtrlZOptions = &ctrlz.Options{
		Address: "localhost",
		Port:    uint16(basePort + 13),
	}

	// Load the mesh config. Note that the path is slightly changed - attempting to move all istio
	// related under /var/lib/istio, which is also the home dir of the istio user.
	istiods, err := bootstrap.NewServer(*args)
	if err != nil {
		log.Fatalf("Failed to start istiod: %v", err)
	}

	err = istiods.Start(stop)
	if err != nil {
		log.Fatalf("Failed on start XDS server: %v", err)
	}

	<-stop
}
