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
	"net/url"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/server/components"
	"istio.io/istio/galley/pkg/server/settings"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/controller/workloadentry"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"
)

// URL schemes supported by the config store
type ConfigSourceAddressScheme string

const (
	// fs:///PATH will load local files. This replaces --configDir.
	// example fs:///tmp/configroot
	// PATH can be mounted from a config map or volume
	File ConfigSourceAddressScheme = "fs"
	// xds://ADDRESS - load XDS-over-MCP sources
	// example xds://127.0.0.1:49133
	XDS ConfigSourceAddressScheme = "xds"
	// k8s:// - load in-cluster k8s controller
	// example k8s://
	Kubernetes ConfigSourceAddressScheme = "k8s"
)

// initConfigController creates the config controller in the pilotConfig.
func (s *Server) initConfigController(args *PilotArgs) error {
	s.initStatusController(args, features.EnableStatus)
	meshConfig := s.environment.Mesh()
	if len(meshConfig.ConfigSources) > 0 {
		// Using MCP for config.
		if err := s.initConfigSources(args); err != nil {
			return err
		}
	} else if args.RegistryOptions.FileDir != "" {
		// Local files - should be added even if other options are specified
		store := memory.Make(collections.Pilot)
		configController := memory.NewController(store)

		err := s.makeFileMonitor(args.RegistryOptions.FileDir, args.RegistryOptions.KubeOptions.DomainSuffix, configController)
		if err != nil {
			return err
		}
		s.ConfigStores = append(s.ConfigStores, configController)
	} else {
		err2 := s.initK8SConfigStore(args)
		if err2 != nil {
			return err2
		}
	}

	// If running in ingress mode (requires k8s), wrap the config controller.
	if hasKubeRegistry(args.RegistryOptions.Registries) && meshConfig.IngressControllerMode != meshconfig.MeshConfig_OFF {
		// Wrap the config controller with a cache.
		s.ConfigStores = append(s.ConfigStores,
			ingress.NewController(s.kubeClient, s.environment.Watcher, args.RegistryOptions.KubeOptions))

		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.IngressController, s.kubeClient.Kube()).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					ingressSyncer := ingress.NewStatusSyncer(s.environment.Watcher, s.kubeClient)
					// Start informers again. This fixes the case where informers for namespace do not start,
					// as we create them only after acquiring the leader lock
					// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
					// basically lazy loading the informer, if we stop it when we lose the lock we will never
					// recreate it again.
					s.kubeClient.RunAndWait(stop)
					log.Infof("Starting ingress controller")
					ingressSyncer.Run(leaderStop)
				}).
				Run(stop)
			return nil
		})
	}

	// Wrap the config controller with a cache.
	aggregateConfigController, err := configaggregate.MakeCache(s.ConfigStores)
	if err != nil {
		return err
	}
	s.configController = aggregateConfigController

	// Create the config store.
	s.environment.IstioConfigStore = model.MakeIstioStore(s.configController)

	// Defer starting the controller until after the service is created.
	s.addStartFunc(func(stop <-chan struct{}) error {
		go s.configController.Run(stop)
		return nil
	})

	return nil
}

func (s *Server) initK8SConfigStore(args *PilotArgs) error {
	if s.kubeClient == nil {
		return nil
	}
	configController, err := s.makeKubeConfigController(args)
	if err != nil {
		return err
	}
	s.ConfigStores = append(s.ConfigStores, configController)
	if features.EnableServiceApis {
		s.ConfigStores = append(s.ConfigStores, gateway.NewController(s.kubeClient, configController, args.RegistryOptions.KubeOptions))
	}
	if features.EnableAnalysis {
		if err := s.initInprocessAnalysisController(args); err != nil {
			return err
		}
	}
	s.XDSServer.WorkloadEntryController = workloadentry.NewController(configController, args.PodName)
	return nil
}

// initConfigSources will process mesh config 'configSources' and initialize
// associated configs.
func (s *Server) initConfigSources(args *PilotArgs) (err error) {
	for _, configSource := range s.environment.Mesh().ConfigSources {
		srcAddress, err := url.Parse(configSource.Address)
		if err != nil {
			return fmt.Errorf("invalid config URL %s %v", configSource.Address, err)
		}
		scheme := ConfigSourceAddressScheme(srcAddress.Scheme)
		switch scheme {
		case File:
			if srcAddress.Path == "" {
				return fmt.Errorf("invalid fs config URL %s, contains no file path", configSource.Address)
			}
			store := memory.MakeSkipValidation(collections.Pilot, false)
			configController := memory.NewController(store)

			err := s.makeFileMonitor(srcAddress.Path, args.RegistryOptions.KubeOptions.DomainSuffix, configController)
			if err != nil {
				return err
			}
			s.ConfigStores = append(s.ConfigStores, configController)
		case XDS:
			xdsMCP, err := adsc.New(srcAddress.Host, &adsc.Config{
				Meta: model.NodeMetadata{
					Generator: "api",
				}.ToStruct(),
				InitialDiscoveryRequests: adsc.ConfigInitialRequests(),
			})
			if err != nil {
				return fmt.Errorf("failed to dial XDS %s %v", configSource.Address, err)
			}
			store := memory.Make(collections.Pilot)
			configController := memory.NewController(store)
			xdsMCP.Store = model.MakeIstioStore(configController)
			err = xdsMCP.Run()
			if err != nil {
				return fmt.Errorf("MCP: failed running %v", err)
			}
			s.ConfigStores = append(s.ConfigStores, configController)
			log.Warn("Started XDS config ", s.ConfigStores)
		case Kubernetes:
			if srcAddress.Path == "" || srcAddress.Path == "/" {
				err2 := s.initK8SConfigStore(args)
				if err2 != nil {
					log.Warn("Error loading k8s ", err2)
					return err2
				}
				log.Warn("Started K8S config")
			} else {
				log.Warnf("Not implemented, ignore: %v", configSource.Address)
				// TODO: handle k8s:// scheme for remote cluster. Use same mechanism as service registry,
				// using the cluster name as key to match a secret.
			}
		default:
			log.Warnf("Ignoring unsupported config source: %v", configSource.Address)
		}
	}
	return nil
}

// initInprocessAnalysisController spins up an instance of Galley which serves no purpose other than
// running Analyzers for status updates.  The Status Updater will eventually need to allow input from istiod
// to support config distribution status as well.
func (s *Server) initInprocessAnalysisController(args *PilotArgs) error {

	processingArgs := settings.DefaultArgs()
	processingArgs.KubeConfig = args.RegistryOptions.KubeConfig
	processingArgs.WatchedNamespaces = args.RegistryOptions.KubeOptions.WatchedNamespaces
	processingArgs.MeshConfigFile = args.MeshConfigFile
	processingArgs.EnableConfigAnalysis = true

	processing := components.NewProcessing(processingArgs)

	s.addStartFunc(func(stop <-chan struct{}) error {
		go leaderelection.
			NewLeaderElection(args.Namespace, args.PodName, leaderelection.AnalyzeController, s.kubeClient).
			AddRunFunction(func(stop <-chan struct{}) {
				if err := processing.Start(); err != nil {
					log.Fatalf("Error starting Background Analysis: %s", err)
				}
				<-stop
				processing.Stop()
			}).Run(stop)
		return nil
	})
	return nil
}

func (s *Server) initStatusController(args *PilotArgs, writeStatus bool) {
	s.statusReporter = &status.Reporter{
		UpdateInterval: time.Millisecond * 500, // TODO: use args here?
		PodName:        args.PodName,
	}
	s.statusReporter.Init(s.environment.GetLedger())
	s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
		if writeStatus {
			s.statusReporter.Start(s.kubeClient, args.Namespace, args.PodName, stop)
		}
		return nil
	})
	s.XDSServer.StatusReporter = s.statusReporter
	if writeStatus {
		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			controller := status.NewController(*s.kubeRestConfig, args.Namespace)
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.StatusController, s.kubeClient).
				AddRunFunction(func(stop <-chan struct{}) {
					controller.Start(stop)
				}).Run(stop)
			return nil
		})
	}
}

func (s *Server) makeKubeConfigController(args *PilotArgs) (model.ConfigStoreCache, error) {
	c, err := crdclient.New(s.kubeClient, args.Revision, args.RegistryOptions.KubeOptions)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Server) makeFileMonitor(fileDir string, domainSuffix string, configController model.ConfigStore) error {
	fileSnapshot := configmonitor.NewFileSnapshot(fileDir, collections.Pilot, domainSuffix)
	fileMonitor := configmonitor.NewMonitor("file-monitor", configController, fileSnapshot.ReadConfigFiles, fileDir)

	// Defer starting the file monitor until after the service is created.
	s.addStartFunc(func(stop <-chan struct{}) error {
		fileMonitor.Start(stop)
		return nil
	})

	return nil
}
