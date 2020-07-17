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

package bootstrap

import (
	"fmt"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/consul"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/mock"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/host"
)

func (s *Server) ServiceController() *aggregate.Controller {
	return s.environment.ServiceDiscovery.(*aggregate.Controller)
}

// initServiceControllers creates and initializes the service controllers
func (s *Server) initServiceControllers(args *PilotArgs) error {
	serviceControllers := s.ServiceController()
	registered := make(map[serviceregistry.ProviderID]bool)
	for _, r := range args.RegistryOptions.Registries {
		serviceRegistry := serviceregistry.ProviderID(r)
		if _, exists := registered[serviceRegistry]; exists {
			log.Warnf("%s registry specified multiple times.", r)
			continue
		}
		registered[serviceRegistry] = true
		log.Infof("Adding %s registry adapter", serviceRegistry)
		switch serviceRegistry {
		case serviceregistry.Kubernetes:
			if err := s.initKubeRegistry(serviceControllers, args); err != nil {
				return err
			}
		case serviceregistry.Consul:
			if err := s.initConsulRegistry(serviceControllers, args); err != nil {
				return err
			}
		case serviceregistry.Mock:
			s.initMockRegistry(serviceControllers)
		default:
			return fmt.Errorf("service registry %s is not supported", r)
		}
	}

	s.serviceEntryStore = serviceentry.NewServiceDiscovery(s.configController, s.environment.IstioConfigStore, s.EnvoyXdsServer)
	serviceControllers.AddRegistry(s.serviceEntryStore)

	if features.EnableServiceEntrySelectPods && s.kubeRegistry != nil {
		// Add an instance handler in the kubernetes registry to notify service entry store about pod events
		_ = s.kubeRegistry.AppendWorkloadHandler(s.serviceEntryStore.WorkloadInstanceHandler)
	}

	if features.EnableK8SServiceSelectWorkloadEntries && s.kubeRegistry != nil {
		// Add an instance handler in the service entry store to notify kubernetes about workload entry events
		_ = s.serviceEntryStore.AppendWorkloadHandler(s.kubeRegistry.WorkloadInstanceHandler)
	}

	// Defer running of the service controllers.
	s.addStartFunc(func(stop <-chan struct{}) error {
		go serviceControllers.Run(stop)
		return nil
	})

	return nil
}

// initKubeRegistry creates all the k8s service controllers under this pilot
func (s *Server) initKubeRegistry(serviceControllers *aggregate.Controller, args *PilotArgs) (err error) {
	args.RegistryOptions.KubeOptions.ClusterID = s.clusterID
	args.RegistryOptions.KubeOptions.Metrics = s.environment
	args.RegistryOptions.KubeOptions.XDSUpdater = s.EnvoyXdsServer
	args.RegistryOptions.KubeOptions.NetworksWatcher = s.environment.NetworksWatcher
	if features.EnableEndpointSliceController {
		args.RegistryOptions.KubeOptions.EndpointMode = kubecontroller.EndpointSliceOnly
	} else {
		args.RegistryOptions.KubeOptions.EndpointMode = kubecontroller.EndpointsOnly
	}

	kubeRegistry := kubecontroller.NewController(s.kubeClient, args.RegistryOptions.KubeOptions)
	s.kubeRegistry = kubeRegistry
	serviceControllers.AddRegistry(kubeRegistry)
	return
}

func (s *Server) initConsulRegistry(serviceControllers *aggregate.Controller, args *PilotArgs) error {
	log.Infof("Consul url: %v", args.RegistryOptions.ConsulServerAddr)
	controller, err := consul.NewController(args.RegistryOptions.ConsulServerAddr, "")
	if err != nil {
		return fmt.Errorf("failed to create Consul controller: %v", err)
	}
	serviceControllers.AddRegistry(controller)

	return nil
}

func (s *Server) initMockRegistry(serviceControllers *aggregate.Controller) {
	// MemServiceDiscovery implementation
	discovery := mock.NewDiscovery(map[host.Name]*model.Service{}, 2)

	registry := serviceregistry.Simple{
		ProviderID:       serviceregistry.Mock,
		ServiceDiscovery: discovery,
		Controller:       &mock.Controller{},
	}

	serviceControllers.AddRegistry(registry)
}
