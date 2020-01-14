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

package bootstrap

import (
	"fmt"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/consul"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/host"
)

func (s *Server) ServiceController() *aggregate.Controller {
	return s.environment.ServiceDiscovery.(*aggregate.Controller)
}

// initServiceControllers creates and initializes the service controllers
func (s *Server) initServiceControllers(args *PilotArgs) error {
	serviceControllers := s.ServiceController()
	registered := make(map[serviceregistry.ProviderID]bool)
	for _, r := range args.Service.Registries {
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
			s.initMemoryRegistry(serviceControllers)
		default:
			return fmt.Errorf("service registry %s is not supported", r)
		}
	}

	s.serviceEntryStore = external.NewServiceDiscovery(s.configController, s.environment.IstioConfigStore, s.EnvoyXdsServer)
	serviceControllers.AddRegistry(s.serviceEntryStore)

	// Defer running of the service controllers.
	s.addStartFunc(func(stop <-chan struct{}) error {
		go serviceControllers.Run(stop)
		return nil
	})

	return nil
}

// initKubeRegistry creates all the k8s service controllers under this pilot
func (s *Server) initKubeRegistry(serviceControllers *aggregate.Controller, args *PilotArgs) (err error) {
	args.Config.ControllerOptions.ClusterID = s.clusterID
	args.Config.ControllerOptions.Metrics = s.environment
	args.Config.ControllerOptions.XDSUpdater = s.EnvoyXdsServer
	args.Config.ControllerOptions.NetworksWatcher = s.environment.NetworksWatcher
	if features.EnableEndpointSliceController {
		args.Config.ControllerOptions.EndpointMode = kubecontroller.EndpointSliceOnly
	} else {
		args.Config.ControllerOptions.EndpointMode = kubecontroller.EndpointsOnly
	}
	kubeRegistry := kubecontroller.NewController(s.kubeClient, args.Config.ControllerOptions)
	s.kubeRegistry = kubeRegistry
	serviceControllers.AddRegistry(kubeRegistry)
	return
}

func (s *Server) initConsulRegistry(serviceControllers *aggregate.Controller, args *PilotArgs) error {
	log.Infof("Consul url: %v", args.Service.Consul.ServerURL)
	conctl, conerr := consul.NewController(args.Service.Consul.ServerURL, "")
	if conerr != nil {
		return fmt.Errorf("failed to create Consul controller: %v", conerr)
	}
	serviceControllers.AddRegistry(conctl)

	return nil
}

func (s *Server) initMemoryRegistry(serviceControllers *aggregate.Controller) {
	// MemServiceDiscovery implementation
	discovery := memory.NewDiscovery(map[host.Name]*model.Service{}, 2)

	registry := serviceregistry.Simple{
		ProviderID:       serviceregistry.Mock,
		ServiceDiscovery: discovery,
		Controller:       &memory.MockController{},
	}

	serviceControllers.AddRegistry(registry)
}
