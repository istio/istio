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
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"istio.io/istio/galley/pkg/server/components"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/adsc"

	"google.golang.org/grpc/keepalive"

	"istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pilot/pkg/features"

	"google.golang.org/grpc"

	mcpapi "istio.io/api/mcp/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/mcp"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/collections"
	configz "istio.io/istio/pkg/mcp/configz/client"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/sink"
)

const (
	// URL types supported by the config store
	// example fs:///tmp/configroot
	fsScheme = "fs"

	requiredMCPCertCheckFreq = 500 * time.Millisecond
)

// initConfigController creates the config controller in the pilotConfig.
func (s *Server) initConfigController(args *PilotArgs) error {
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

	// Used for tests.
	memStore := memory.Make(collections.Pilot)
	memConfigController := memory.NewController(memStore)
	s.ConfigStores = append(s.ConfigStores, memConfigController)
	s.EnvoyXdsServer.MemConfigController = memConfigController

	// If running in ingress mode (requires k8s), wrap the config controller.
	if hasKubeRegistry(args.RegistryOptions.Registries) && meshConfig.IngressControllerMode != meshconfig.MeshConfig_OFF {
		// Wrap the config controller with a cache.
		s.ConfigStores = append(s.ConfigStores,
			ingress.NewController(s.kubeClient, s.environment.Watcher, args.RegistryOptions.KubeOptions))

		ingressSyncer, err := ingress.NewStatusSyncer(meshConfig, s.kubeClient)
		if err != nil {
			log.Warnf("Disabled ingress status syncer due to %v", err)
		} else {
			s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
				leaderelection.
					NewLeaderElection(args.Namespace, args.PodName, leaderelection.IngressController, s.kubeClient.Kube()).
					AddRunFunction(func(leaderStop <-chan struct{}) {
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
	s.initStatusController(args, features.EnableStatus)
	return nil
}

// initConfigSources will process mesh config 'configSources' and initialize
// associated configs.
//
// - fs:///PATH will load local files. This replaces --configDir.
//   PATH can be mounted from a config map or volume
//
// - k8s:// - load in-cluster k8s controller.
//
// - xds://ADDRESS - load XDS-over-MCP sources
//
// -
func (s *Server) initConfigSources(args *PilotArgs) (err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	var clients []*sink.Client
	var conns []*grpc.ClientConn

	mcpOptions := &mcp.Options{
		DomainSuffix: args.RegistryOptions.KubeOptions.DomainSuffix,
		ConfigLedger: buildLedger(args.RegistryOptions),
		XDSUpdater:   s.EnvoyXdsServer,
		Revision:     args.Revision,
	}
	reporter := monitoring.NewStatsContext("pilot")

	for _, configSource := range s.environment.Mesh().ConfigSources {
		if strings.Contains(configSource.Address, fsScheme+"://") {
			srcAddress, err := url.Parse(configSource.Address)
			if err != nil {
				return fmt.Errorf("invalid config URL %s %v", configSource.Address, err)
			}
			if srcAddress.Scheme == fsScheme {
				if srcAddress.Path == "" {
					return fmt.Errorf("invalid fs config URL %s, contains no file path", configSource.Address)
				}
				store := memory.MakeWithLedger(collections.Pilot, buildLedger(args.RegistryOptions), false)
				configController := memory.NewController(store)

				err := s.makeFileMonitor(srcAddress.Path, args.RegistryOptions.KubeOptions.DomainSuffix, configController)
				if err != nil {
					return err
				}
				s.ConfigStores = append(s.ConfigStores, configController)
				continue
			}
		}
		if strings.Contains(configSource.Address, "xds://") {
			srcAddress, err := url.Parse(configSource.Address)
			if err != nil {
				return fmt.Errorf("invalid XDS config URL %s %v", configSource.Address, err)
			}
			// TODO: use a query param or schema to specify insecure
			xdsMCP, err := adsc.New(&meshconfig.ProxyConfig{
				DiscoveryAddress: srcAddress.Host,
			}, &adsc.Config{
				Meta: model.NodeMetadata{
					Generator: "api",
				}.ToStruct(),
			})
			store := memory.Make(collections.Pilot)
			configController := memory.NewController(store)
			xdsMCP.Store = model.MakeIstioStore(configController)

			if err != nil {
				return fmt.Errorf("failed to dial XDS %s %v", configSource.Address, err)
			}
			go xdsMCP.WatchConfig()
			s.ConfigStores = append(s.ConfigStores, configController)
			log.Warna("Started XDS config ", s.ConfigStores)
			continue
		}
		if strings.Contains(configSource.Address, "k8s://") {
			srcAddress, err := url.Parse(configSource.Address)
			if err != nil {
				return fmt.Errorf("invalid K8S config URL %s %v", configSource.Address, err)
			}
			if srcAddress.Path == "" || srcAddress.Path == "/" {
				err2 := s.initK8SConfigStore(args)
				if err2 != nil {
					log.Warna("Error loading k8s ", err2)
					return err2
				}
				log.Warn("Started K8S config")
			} else {
				log.Warnf("Not implemented, ignore: %v", configSource.Address)
				// TODO: handle k8s:// scheme for remote cluster. Use same mechanism as service registry,
				// using the cluster name as key to match a secret.
			}
			continue
		}

		conn, err := grpcDialMCP(ctx, configSource, args)
		if err != nil {
			log.Errorf("Unable to dial MCP Server %q: %v", configSource.Address, err)
			return err
		}
		conns = append(conns, conn)
		mcpController, mcpClient := s.mcpController(mcpOptions, conn, reporter)
		clients = append(clients, mcpClient)
		s.ConfigStores = append(s.ConfigStores, mcpController)
	}

	s.addStartFunc(func(stop <-chan struct{}) error {
		var wg sync.WaitGroup

		for i := range clients {
			client := clients[i]
			wg.Add(1)
			go func() {
				defer wg.Done()
				client.Run(ctx)
			}()
		}

		go func() {
			<-stop

			// Stop the MCP clients and any pending connection.
			cancel()

			// Close all of the open grpc connections once the mcp
			// client(s) have fully stopped.
			wg.Wait()
			for _, conn := range conns {
				_ = conn.Close() // nolint: errcheck
			}

			_ = reporter.Close()
		}()

		return nil
	})
	return nil
}

func mcpSecurityOptions(ctx context.Context, configSource *meshconfig.ConfigSource) (grpc.DialOption, error) {
	securityOption := grpc.WithInsecure()
	if configSource.TlsSettings != nil &&
		configSource.TlsSettings.Mode != networkingapi.ClientTLSSettings_DISABLE {
		var credentialOption *creds.Options
		switch configSource.TlsSettings.Mode {
		case networkingapi.ClientTLSSettings_SIMPLE:
		case networkingapi.ClientTLSSettings_MUTUAL:
			credentialOption = &creds.Options{
				CertificateFile:   configSource.TlsSettings.ClientCertificate,
				KeyFile:           configSource.TlsSettings.PrivateKey,
				CACertificateFile: configSource.TlsSettings.CaCertificates,
			}
		case networkingapi.ClientTLSSettings_ISTIO_MUTUAL:
			credentialOption = &creds.Options{
				CertificateFile:   path.Join(constants.AuthCertsPath, constants.CertChainFilename),
				KeyFile:           path.Join(constants.AuthCertsPath, constants.KeyFilename),
				CACertificateFile: path.Join(constants.AuthCertsPath, constants.RootCertFilename),
			}
		default:
			log.Errorf("invalid tls setting mode %d", configSource.TlsSettings.Mode)
		}

		if credentialOption == nil {
			transportCreds := creds.CreateForClientSkipVerify()
			securityOption = grpc.WithTransportCredentials(transportCreds)
		} else {
			requiredFiles := []string{
				credentialOption.CACertificateFile,
				credentialOption.KeyFile,
				credentialOption.CertificateFile}
			log.Infof("Secure MCP configured. Waiting for required certificate files to become available: %v",
				requiredFiles)
			for len(requiredFiles) > 0 {
				if _, err := os.Stat(requiredFiles[0]); os.IsNotExist(err) {
					log.Infof("%v not found. Checking again in %v", requiredFiles[0], requiredMCPCertCheckFreq)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(requiredMCPCertCheckFreq):
						// retry
						continue
					}
				}
				log.Debugf("MCP certificate file %s found", requiredFiles[0])
				requiredFiles = requiredFiles[1:]
			}

			watcher, err := creds.WatchFiles(ctx.Done(), credentialOption)
			if err != nil {
				return nil, err
			}
			transportCreds := creds.CreateForClient(configSource.TlsSettings.Sni, watcher)
			securityOption = grpc.WithTransportCredentials(transportCreds)
		}
	}
	return securityOption, nil
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
	s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
		s.statusReporter.Start(s.kubeClient, args.Namespace, s.configController, writeStatus, stop)
		return nil
	})
	s.EnvoyXdsServer.StatusReporter = s.statusReporter
	if writeStatus {
		s.addTerminatingStartFunc(func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.StatusController, s.kubeClient).
				AddRunFunction(func(stop <-chan struct{}) {
					controller := &status.DistributionController{
						QPS:   float32(features.StatusQPS),
						Burst: features.StatusBurst}
					controller.Start(s.kubeRestConfig, args.Namespace, stop)
				}).Run(stop)
			return nil
		})
	}
}

func (s *Server) mcpController(
	opts *mcp.Options,
	conn *grpc.ClientConn,
	reporter monitoring.Reporter) (model.ConfigStoreCache, *sink.Client) {
	clientNodeID := ""
	all := collections.Pilot.All()
	cols := make([]sink.CollectionOptions, 0, len(all))
	for _, c := range all {
		cols = append(cols, sink.CollectionOptions{Name: c.Name().String(), Incremental: features.EnableIncrementalMCP})
	}

	mcpController := mcp.NewController(opts)
	sinkOptions := &sink.Options{
		CollectionOptions: cols,
		Updater:           mcpController,
		ID:                clientNodeID,
		Reporter:          reporter,
	}

	cl := mcpapi.NewResourceSourceClient(conn)
	mcpClient := sink.NewClient(cl, sinkOptions)
	configz.Register(mcpClient)
	return mcpController, mcpClient
}

func (s *Server) makeKubeConfigController(args *PilotArgs) (model.ConfigStoreCache, error) {
	c, err := crdclient.New(s.kubeClient, buildLedger(args.RegistryOptions), args.Revision, args.RegistryOptions.KubeOptions)
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

// Note: MCP is in process of getting replaced with MCP-over-XDS
func grpcDialMCP(ctx context.Context,
	configSource *meshconfig.ConfigSource, args *PilotArgs) (*grpc.ClientConn, error) {
	securityOption, err := mcpSecurityOptions(ctx, configSource)
	if err != nil {
		return nil, err
	}

	keepaliveOption := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    args.KeepaliveOptions.Time,
		Timeout: args.KeepaliveOptions.Timeout,
	})

	initialWindowSizeOption := grpc.WithInitialWindowSize(int32(args.MCPOptions.InitialWindowSize))
	initialConnWindowSizeOption := grpc.WithInitialConnWindowSize(int32(args.MCPOptions.InitialConnWindowSize))
	msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(args.MCPOptions.MaxMessageSize))

	return grpc.DialContext(
		ctx,
		configSource.Address,
		securityOption,
		msgSizeOption,
		keepaliveOption,
		initialWindowSizeOption,
		initialConnWindowSizeOption)
}
