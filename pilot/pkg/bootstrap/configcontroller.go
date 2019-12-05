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
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"

	mcpapi "istio.io/api/mcp/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/config/kube/crd/controller"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	configmonitor "istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schemas"
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
		if err := s.initMCPConfigController(args); err != nil {
			return err
		}
	} else if args.Config.FileDir != "" {
		store := memory.Make(schemas.Istio)
		configController := memory.NewController(store)

		err := s.makeFileMonitor(args.Config.FileDir, configController)
		if err != nil {
			return err
		}
		s.ConfigStores = append(s.ConfigStores, configController)
	} else {
		configController, err := s.makeKubeConfigController(args)
		if err != nil {
			return err
		}
		s.ConfigStores = append(s.ConfigStores, configController)
	}

	// If running in ingress mode (requires k8s), wrap the config controller.
	if hasKubeRegistry(args.Service.Registries) && meshConfig.IngressControllerMode != meshconfig.MeshConfig_OFF {
		// Wrap the config controller with a cache.
		s.ConfigStores = append(s.ConfigStores,
			ingress.NewController(s.kubeClient, meshConfig, args.Config.ControllerOptions))

		if ingressSyncer, errSyncer := ingress.NewStatusSyncer(meshConfig, s.kubeClient,
			args.Namespace, args.Config.ControllerOptions); errSyncer != nil {
			log.Warnf("Disabled ingress status syncer due to %v", errSyncer)
		} else {
			s.addStartFunc(func(stop <-chan struct{}) error {
				go ingressSyncer.Run(stop)
				return nil
			})
		}
	}

	// Wrap the config controller with a cache.
	aggregateMcpController, err := configaggregate.MakeCache(s.ConfigStores)
	if err != nil {
		return err
	}
	s.configController = aggregateMcpController

	// Create the config store.
	s.environment.IstioConfigStore = model.MakeIstioStore(s.configController)

	// Defer starting the controller until after the service is created.
	s.addStartFunc(func(stop <-chan struct{}) error {
		go s.configController.Run(stop)
		return nil
	})

	return nil
}

func (s *Server) initMCPConfigController(args *PilotArgs) error {
	ctx, cancel := context.WithCancel(context.Background())
	var clients []*sink.Client
	var conns []*grpc.ClientConn
	var configStores []model.ConfigStoreCache

	s.mcpOptions = &coredatamodel.Options{
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
		ConfigLedger: buildLedger(args.Config),
		XDSUpdater:   s.EnvoyXdsServer,
	}
	reporter := monitoring.NewStatsContext("pilot")

	for _, configSource := range s.environment.Mesh().ConfigSources {
		if strings.Contains(configSource.Address, fsScheme+"://") {
			srcAddress, err := url.Parse(configSource.Address)
			if err != nil {
				cancel()
				return fmt.Errorf("invalid config URL %s %v", configSource.Address, err)
			}
			if srcAddress.Scheme == fsScheme {
				if srcAddress.Path == "" {
					cancel()
					return fmt.Errorf("invalid fs config URL %s, contains no file path", configSource.Address)
				}
				store := memory.MakeWithLedger(schemas.Istio, buildLedger(args.Config))
				configController := memory.NewController(store)

				err := s.makeFileMonitor(srcAddress.Path, configController)
				if err != nil {
					cancel()
					return err
				}
				configStores = append(configStores, configController)
				continue
			}
		}

		conn, err := grpcDial(ctx, cancel, configSource, args)
		if err != nil {
			log.Errorf("Unable to dial MCP Server %q: %v", configSource.Address, err)
			cancel()
			return err
		}
		conns = append(conns, conn)
		s.mcpController(conn, reporter, &clients, &configStores)

		// create MCP SyntheticServiceEntryController
		if resourceContains(configSource.SubscribedResources, meshconfig.Resource_SERVICE_REGISTRY) {
			conn, err := grpcDial(ctx, cancel, configSource, args)
			if err != nil {
				log.Errorf("Unable to dial MCP Server %q: %v", configSource.Address, err)
				cancel()
				return err
			}
			conns = append(conns, conn)
			s.sseMCPController(args, conn, reporter, &clients, &configStores)
		}
	}

	s.addStartFunc(func(stop <-chan struct{}) error {
		var wg sync.WaitGroup

		for i := range clients {
			client := clients[i]
			wg.Add(1)
			go func() {
				client.Run(ctx)
				wg.Done()
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

	s.ConfigStores = append(s.ConfigStores, configStores...)
	return nil
}

func resourceContains(resources []meshconfig.Resource, resource meshconfig.Resource) bool {
	for _, r := range resources {
		if r == resource {
			return true
		}
	}
	return false
}

func mcpSecurityOptions(ctx context.Context, cancel context.CancelFunc, configSource *meshconfig.ConfigSource) (grpc.DialOption, error) {
	securityOption := grpc.WithInsecure()
	if configSource.TlsSettings != nil &&
		configSource.TlsSettings.Mode != networkingapi.TLSSettings_DISABLE {
		var credentialOption *creds.Options
		switch configSource.TlsSettings.Mode {
		case networkingapi.TLSSettings_SIMPLE:
		case networkingapi.TLSSettings_MUTUAL:
			credentialOption = &creds.Options{
				CertificateFile:   configSource.TlsSettings.ClientCertificate,
				KeyFile:           configSource.TlsSettings.PrivateKey,
				CACertificateFile: configSource.TlsSettings.CaCertificates,
			}
		case networkingapi.TLSSettings_ISTIO_MUTUAL:
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
						cancel()
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
				cancel()
				return nil, err
			}
			transportCreds := creds.CreateForClient(configSource.TlsSettings.Sni, watcher)
			securityOption = grpc.WithTransportCredentials(transportCreds)
		}
	}
	return securityOption, nil
}

func (s *Server) mcpController(
	conn *grpc.ClientConn,
	reporter monitoring.Reporter,
	clients *[]*sink.Client,
	configStores *[]model.ConfigStoreCache) {
	clientNodeID := ""
	collections := make([]sink.CollectionOptions, 0, len(schemas.Istio)-1)
	for _, c := range schemas.Istio {
		// do not register SSEs for this controller as there is a dedicated controller
		if c.Collection == schemas.SyntheticServiceEntry.Collection {
			continue
		}
		collections = append(collections, sink.CollectionOptions{Name: c.Collection, Incremental: false})
	}

	mcpController := coredatamodel.NewController(s.mcpOptions)
	sinkOptions := &sink.Options{
		CollectionOptions: collections,
		Updater:           mcpController,
		ID:                clientNodeID,
		Reporter:          reporter,
	}

	cl := mcpapi.NewResourceSourceClient(conn)
	mcpClient := sink.NewClient(cl, sinkOptions)
	configz.Register(mcpClient)
	*clients = append(*clients, mcpClient)
	*configStores = append(*configStores, mcpController)
}

func (s *Server) sseMCPController(args *PilotArgs,
	conn *grpc.ClientConn,
	reporter monitoring.Reporter,
	clients *[]*sink.Client,
	configStores *[]model.ConfigStoreCache) {
	clientNodeID := "SSEMCP"
	s.incrementalMcpOptions = &coredatamodel.Options{
		ClusterID:    s.clusterID,
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
		XDSUpdater:   s.EnvoyXdsServer,
	}
	ctl := coredatamodel.NewSyntheticServiceEntryController(s.incrementalMcpOptions)
	s.discoveryOptions = &coredatamodel.DiscoveryOptions{
		ClusterID:    s.clusterID,
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
	}
	s.mcpDiscovery = coredatamodel.NewMCPDiscovery(ctl, s.discoveryOptions)
	incrementalSinkOptions := &sink.Options{
		CollectionOptions: []sink.CollectionOptions{
			{
				Name:        schemas.SyntheticServiceEntry.Collection,
				Incremental: true,
			},
		},
		Updater:  ctl,
		ID:       clientNodeID,
		Reporter: reporter,
	}
	incSrcClient := mcpapi.NewResourceSourceClient(conn)
	incMcpClient := sink.NewClient(incSrcClient, incrementalSinkOptions)
	configz.Register(incMcpClient)
	*clients = append(*clients, incMcpClient)
	*configStores = append(*configStores, ctl)
}

func (s *Server) makeKubeConfigController(args *PilotArgs) (model.ConfigStoreCache, error) {
	configClient, err := controller.NewClient(args.Config.KubeConfig, "", schemas.Istio,
		args.Config.ControllerOptions.DomainSuffix, buildLedger(args.Config))
	if err != nil {
		return nil, multierror.Prefix(err, "failed to open a config client.")
	}

	if !args.Config.DisableInstallCRDs {
		if err = configClient.RegisterResources(); err != nil {
			return nil, multierror.Prefix(err, "failed to register custom resources.")
		}
	}

	return controller.NewController(configClient, args.Config.ControllerOptions), nil
}

func (s *Server) makeFileMonitor(fileDir string, configController model.ConfigStore) error {
	fileSnapshot := configmonitor.NewFileSnapshot(fileDir, schemas.Istio)
	fileMonitor := configmonitor.NewMonitor("file-monitor", configController, FilepathWalkInterval, fileSnapshot.ReadConfigFiles)

	// Defer starting the file monitor until after the service is created.
	s.addStartFunc(func(stop <-chan struct{}) error {
		fileMonitor.Start(stop)
		return nil
	})

	return nil
}
