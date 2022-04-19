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

	meshconfig "istio.io/api/mesh/v1alpha1"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	ingressv1 "istio.io/istio/pilot/pkg/config/kube/ingressv1"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/controller/workloadentry"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/status/distribution"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/analysis/incluster"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
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
		// Supporting only Ingress/v1 means we lose support of Kubernetes 1.18
		// Supporting only Ingress/v1beta1 means we lose support of Kubernetes 1.22
		// Since supporting both in a monolith controller is painful due to lack of usable conversion logic between
		// the two versions.
		// As a compromise, we instead just fork the controller. Once 1.18 support is no longer needed, we can drop the old controller
		ingressV1 := ingress.V1Available(s.kubeClient)
		if ingressV1 {
			s.ConfigStores = append(s.ConfigStores,
				ingressv1.NewController(s.kubeClient, s.environment.Watcher, args.RegistryOptions.KubeOptions))
		} else {
			s.ConfigStores = append(s.ConfigStores,
				ingress.NewController(s.kubeClient, s.environment.Watcher, args.RegistryOptions.KubeOptions))
		}

		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.IngressController, args.Revision, s.kubeClient).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					if ingressV1 {
						ingressSyncer := ingressv1.NewStatusSyncer(s.environment.Watcher, s.kubeClient)
						// Start informers again. This fixes the case where informers for namespace do not start,
						// as we create them only after acquiring the leader lock
						// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
						// basically lazy loading the informer, if we stop it when we lose the lock we will never
						// recreate it again.
						s.kubeClient.RunAndWait(stop)
						log.Infof("Starting ingress controller")
						ingressSyncer.Run(leaderStop)
					} else {
						ingressSyncer := ingress.NewStatusSyncer(s.environment.Watcher, s.kubeClient)
						// Start informers again. This fixes the case where informers for namespace do not start,
						// as we create them only after acquiring the leader lock
						// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
						// basically lazy loading the informer, if we stop it when we lose the lock we will never
						// recreate it again.
						s.kubeClient.RunAndWait(stop)
						log.Infof("Starting ingress controller")
						ingressSyncer.Run(leaderStop)
					}
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
	s.environment.ConfigStore = model.MakeIstioStore(s.configController)

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
	if features.EnableGatewayAPI {
		if s.statusManager == nil && features.EnableGatewayAPIStatus {
			s.initStatusManager(args)
		}
		gwc := gateway.NewController(s.kubeClient, configController, args.RegistryOptions.KubeOptions)
		s.environment.GatewayAPIController = gwc
		s.ConfigStores = append(s.ConfigStores, s.environment.GatewayAPIController)
		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.GatewayStatusController, args.Revision, s.kubeClient).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("Starting gateway status writer")
					gwc.SetStatusWrite(true, s.statusManager)

					// Trigger a push so we can recompute status
					s.XDSServer.ConfigUpdate(&model.PushRequest{
						Full:   true,
						Reason: []model.TriggerReason{model.GlobalUpdate},
					})
					<-leaderStop
					log.Infof("Stopping gateway status writer")
					gwc.SetStatusWrite(false, nil)
				}).
				Run(stop)
			return nil
		})
		if features.EnableGatewayAPIDeploymentController {
			s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
				leaderelection.
					NewLeaderElection(args.Namespace, args.PodName, leaderelection.GatewayDeploymentController, args.Revision, s.kubeClient).
					AddRunFunction(func(leaderStop <-chan struct{}) {
						// We can only run this if the Gateway CRD is created
						if crdclient.WaitForCRD(gvk.KubernetesGateway, leaderStop) {
							controller := gateway.NewDeploymentController(s.kubeClient)
							// Start informers again. This fixes the case where informers for namespace do not start,
							// as we create them only after acquiring the leader lock
							// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
							// basically lazy loading the informer, if we stop it when we lose the lock we will never
							// recreate it again.
							s.kubeClient.RunAndWait(stop)
							controller.Run(leaderStop)
						}
					}).
					Run(stop)
				return nil
			})
		}
	}
	if features.EnableAnalysis {
		if err := s.initInprocessAnalysisController(args); err != nil {
			return err
		}
	}
	s.RWConfigStore, err = configaggregate.MakeWriteableCache(s.ConfigStores, configController)
	if err != nil {
		return err
	}
	s.XDSServer.WorkloadEntryController = workloadentry.NewController(configController, args.PodName, args.KeepaliveOptions.MaxServerConnectionAge)
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
			store := memory.Make(collections.Pilot)
			configController := memory.NewController(store)

			err := s.makeFileMonitor(srcAddress.Path, args.RegistryOptions.KubeOptions.DomainSuffix, configController)
			if err != nil {
				return err
			}
			s.ConfigStores = append(s.ConfigStores, configController)
		case XDS:
			xdsMCP, err := adsc.New(srcAddress.Host, &adsc.Config{
				Namespace: args.Namespace,
				Workload:  args.PodName,
				Revision:  args.Revision,
				Meta: model.NodeMetadata{
					Generator: "api",
					// To reduce transported data if upstream server supports. Especially for custom servers.
					IstioRevision: args.Revision,
				}.ToStruct(),
				InitialDiscoveryRequests: adsc.ConfigInitialRequests(),
			})
			if err != nil {
				return fmt.Errorf("failed to dial XDS %s %v", configSource.Address, err)
			}
			store := memory.Make(collections.Pilot)
			configController := memory.NewController(store)
			configController.RegisterHasSyncedHandler(xdsMCP.HasSynced)
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
	if s.statusManager == nil {
		s.initStatusManager(args)
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		go leaderelection.
			NewLeaderElection(args.Namespace, args.PodName, leaderelection.AnalyzeController, args.Revision, s.kubeClient).
			AddRunFunction(func(stop <-chan struct{}) {
				cont, err := incluster.NewController(stop, s.RWConfigStore,
					s.kubeClient, args.Namespace, s.statusManager, args.RegistryOptions.KubeOptions.DomainSuffix)
				if err != nil {
					return
				}
				cont.Run(stop)
			}).Run(stop)
		return nil
	})
	return nil
}

func (s *Server) initStatusController(args *PilotArgs, writeStatus bool) {
	if s.statusManager == nil && writeStatus {
		s.initStatusManager(args)
	}
	s.statusReporter = &distribution.Reporter{
		UpdateInterval: features.StatusUpdateInterval,
		PodName:        args.PodName,
	}
	s.addStartFunc(func(stop <-chan struct{}) error {
		s.statusReporter.Init(s.environment.GetLedger(), stop)
		return nil
	})
	s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
		if writeStatus {
			s.statusReporter.Start(s.kubeClient, args.Namespace, args.PodName, stop)
		}
		return nil
	})
	s.XDSServer.StatusReporter = s.statusReporter
	if writeStatus {
		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.StatusController, args.Revision, s.kubeClient).
				AddRunFunction(func(stop <-chan struct{}) {
					// Controller should be created for calling the run function every time, so it can
					// avoid concurrently calling of informer Run() for controller in controller.Start
					controller := distribution.NewController(s.kubeClient.RESTConfig(), args.Namespace, s.RWConfigStore, s.statusManager)
					s.statusReporter.SetController(controller)
					controller.Start(stop)
				}).Run(stop)
			return nil
		})
	}
}

func (s *Server) makeKubeConfigController(args *PilotArgs) (model.ConfigStoreController, error) {
	return crdclient.New(s.kubeClient, args.Revision, args.RegistryOptions.KubeOptions.DomainSuffix)
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
