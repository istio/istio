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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/autoregistration"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	ingress "istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/status/distribution"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/analysis/incluster"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/revisions"
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
	s.initStatusController(args, features.EnableStatus && features.EnableDistributionTracking)
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
		err := s.initK8SConfigStore(args)
		if err != nil {
			return err
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
		s.ConfigStores = append(s.ConfigStores,
			ingress.NewController(s.kubeClient, s.environment.Watcher, args.RegistryOptions.KubeOptions))

		s.addTerminatingStartFunc("ingress status", func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.IngressController, args.Revision, s.kubeClient).
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
	s.environment.ConfigStore = aggregateConfigController

	// Defer starting the controller until after the service is created.
	s.addStartFunc("config controller", func(stop <-chan struct{}) error {
		go s.configController.Run(stop)
		return nil
	})

	return nil
}

func (s *Server) initK8SConfigStore(args *PilotArgs) error {
	if s.kubeClient == nil {
		return nil
	}
	configController := s.makeKubeConfigController(args)
	s.ConfigStores = append(s.ConfigStores, configController)
	if features.EnableGatewayAPI {
		if s.statusManager == nil && features.EnableGatewayAPIStatus {
			s.initStatusManager(args)
		}
		gwc := gateway.NewController(s.kubeClient, configController, s.kubeClient.CrdWatcher().WaitForCRD,
			s.environment.CredentialsController, args.RegistryOptions.KubeOptions)
		s.environment.GatewayAPIController = gwc
		s.ConfigStores = append(s.ConfigStores, s.environment.GatewayAPIController)
		s.addTerminatingStartFunc("gateway status", func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.GatewayStatusController, args.Revision, s.kubeClient).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("Starting gateway status writer")
					gwc.SetStatusWrite(true, s.statusManager)

					// Trigger a push so we can recompute status
					s.XDSServer.ConfigUpdate(&model.PushRequest{
						Full:   true,
						Reason: model.NewReasonStats(model.GlobalUpdate),
					})
					<-leaderStop
					log.Infof("Stopping gateway status writer")
					gwc.SetStatusWrite(false, nil)
				}).
				Run(stop)
			return nil
		})
		if features.EnableGatewayAPIDeploymentController {
			s.addTerminatingStartFunc("gateway deployment controller", func(stop <-chan struct{}) error {
				leaderelection.
					NewPerRevisionLeaderElection(args.Namespace, args.PodName, leaderelection.GatewayDeploymentController, args.Revision, s.kubeClient).
					AddRunFunction(func(leaderStop <-chan struct{}) {
						// We can only run this if the Gateway CRD is created
						if s.kubeClient.CrdWatcher().WaitForCRD(gvr.KubernetesGateway, leaderStop) {
							tagWatcher := revisions.NewTagWatcher(s.kubeClient, args.Revision)
							controller := gateway.NewDeploymentController(s.kubeClient, s.clusterID, s.environment,
								s.webhookInfo.getWebhookConfig, s.webhookInfo.addHandler, tagWatcher, args.Revision)
							// Start informers again. This fixes the case where informers for namespace do not start,
							// as we create them only after acquiring the leader lock
							// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
							// basically lazy loading the informer, if we stop it when we lose the lock we will never
							// recreate it again.
							s.kubeClient.RunAndWait(stop)
							go tagWatcher.Run(leaderStop)
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
	var err error
	s.RWConfigStore, err = configaggregate.MakeWriteableCache(s.ConfigStores, configController)
	if err != nil {
		return err
	}
	s.XDSServer.WorkloadEntryController = autoregistration.NewController(configController, args.PodName, args.KeepaliveOptions.MaxServerConnectionAge)
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
			log.Infof("Started File configSource %s", configSource.Address)
		case XDS:
			xdsMCP, err := adsc.New(srcAddress.Host, &adsc.ADSConfig{
				InitialDiscoveryRequests: adsc.ConfigInitialRequests(),
				Config: adsc.Config{
					Namespace: args.Namespace,
					Workload:  args.PodName,
					Revision:  args.Revision,
					Meta: model.NodeMetadata{
						Generator: "api",
						// To reduce transported data if upstream server supports. Especially for custom servers.
						IstioRevision: args.Revision,
					}.ToStruct(),
					GrpcOpts: []grpc.DialOption{
						args.KeepaliveOptions.ConvertToClientOption(),
						// Because we use the custom grpc options for adsc, here we should
						// explicitly set transport credentials.
						// TODO: maybe we should use the tls settings within ConfigSource
						// to secure the connection between istiod and remote xds server.
						grpc.WithTransportCredentials(insecure.NewCredentials()),
					},
				},
			})
			if err != nil {
				return fmt.Errorf("failed to dial XDS %s %v", configSource.Address, err)
			}
			store := memory.Make(collections.Pilot)
			// TODO: enable namespace filter for memory controller
			configController := memory.NewController(store)
			configController.RegisterHasSyncedHandler(xdsMCP.HasSynced)
			xdsMCP.Store = configController
			err = xdsMCP.Run()
			if err != nil {
				return fmt.Errorf("MCP: failed running %v", err)
			}
			s.ConfigStores = append(s.ConfigStores, configController)
			log.Infof("Started XDS configSource %s", configSource.Address)
		case Kubernetes:
			if srcAddress.Path == "" || srcAddress.Path == "/" {
				err2 := s.initK8SConfigStore(args)
				if err2 != nil {
					log.Warnf("Error loading k8s: %v", err2)
					return err2
				}
				log.Infof("Started Kubernetes configSource %s", configSource.Address)
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
	s.addStartFunc("analysis controller", func(stop <-chan struct{}) error {
		go leaderelection.
			NewLeaderElection(args.Namespace, args.PodName, leaderelection.AnalyzeController, args.Revision, s.kubeClient).
			AddRunFunction(func(leaderStop <-chan struct{}) {
				cont, err := incluster.NewController(leaderStop, s.RWConfigStore,
					s.kubeClient, args.Revision, args.Namespace, s.statusManager, args.RegistryOptions.KubeOptions.DomainSuffix)
				if err != nil {
					return
				}
				cont.Run(leaderStop)
			}).Run(stop)
		return nil
	})
	return nil
}

func (s *Server) initStatusController(args *PilotArgs, writeStatus bool) {
	if s.statusManager == nil && writeStatus {
		s.initStatusManager(args)
	}
	if features.EnableDistributionTracking {
		s.statusReporter = &distribution.Reporter{
			UpdateInterval: features.StatusUpdateInterval,
			PodName:        args.PodName,
		}
		s.addStartFunc("status reporter init", func(stop <-chan struct{}) error {
			s.statusReporter.Init(s.environment.GetLedger(), stop)
			return nil
		})
		s.addTerminatingStartFunc("status reporter", func(stop <-chan struct{}) error {
			if writeStatus {
				s.statusReporter.Start(s.kubeClient.Kube(), args.Namespace, args.PodName, stop)
			}
			return nil
		})
		s.XDSServer.StatusReporter = s.statusReporter
	}
	if writeStatus {
		s.addTerminatingStartFunc("status distribution", func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.StatusController, args.Revision, s.kubeClient).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					// Controller should be created for calling the run function every time, so it can
					// avoid concurrently calling of informer Run() for controller in controller.Start
					controller := distribution.NewController(s.kubeClient.RESTConfig(), args.Namespace, s.RWConfigStore, s.statusManager)
					s.statusReporter.SetController(controller)
					controller.Start(leaderStop)
				}).Run(stop)
			return nil
		})
	}
}

func (s *Server) makeKubeConfigController(args *PilotArgs) *crdclient.Client {
	opts := crdclient.Option{
		Revision:     args.Revision,
		DomainSuffix: args.RegistryOptions.KubeOptions.DomainSuffix,
		Identifier:   "crd-controller",
	}
	return crdclient.New(s.kubeClient, opts)
}

func (s *Server) makeFileMonitor(fileDir string, domainSuffix string, configController model.ConfigStore) error {
	fileSnapshot := configmonitor.NewFileSnapshot(fileDir, collections.Pilot, domainSuffix)
	fileMonitor := configmonitor.NewMonitor("file-monitor", configController, fileSnapshot.ReadConfigFiles, fileDir)

	// Defer starting the file monitor until after the service is created.
	s.addStartFunc("file monitor", func(stop <-chan struct{}) error {
		fileMonitor.Start(stop)
		return nil
	})

	return nil
}
