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

	"google.golang.org/grpc/keepalive"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"

	mcpapi "istio.io/api/mcp/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/log"

	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/crd/controller"
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
		if err := s.initMCPConfigController(args); err != nil {
			return err
		}
	} else if args.Config.FileDir != "" {
		store := memory.Make(collections.Pilot)
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

		if ingressSyncer, errSyncer := ingress.NewStatusSyncer(meshConfig, s.kubeClient, args.Config.ControllerOptions, s.leaderElection); errSyncer != nil {
			log.Warnf("Disabled ingress status syncer due to %v", errSyncer)
		} else {
			s.addStartFunc(func(stop <-chan struct{}) error {
				go ingressSyncer.Run(stop)
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

func (s *Server) initMCPConfigController(args *PilotArgs) (err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	var clients []*sink.Client
	var conns []*grpc.ClientConn
	var configStores []model.ConfigStoreCache

	mcpOptions := &mcp.Options{
		DomainSuffix: args.Config.ControllerOptions.DomainSuffix,
		ConfigLedger: buildLedger(args.Config),
		XDSUpdater:   s.EnvoyXdsServer,
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
				store := memory.MakeWithLedger(collections.Pilot, buildLedger(args.Config))
				configController := memory.NewController(store)

				err := s.makeFileMonitor(srcAddress.Path, configController)
				if err != nil {
					return err
				}
				configStores = append(configStores, configController)
				continue
			}
		}

		conn, err := grpcDial(ctx, configSource, args)
		if err != nil {
			log.Errorf("Unable to dial MCP Server %q: %v", configSource.Address, err)
			return err
		}
		conns = append(conns, conn)
		s.mcpController(mcpOptions, conn, reporter, &clients, &configStores)
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

	s.ConfigStores = append(s.ConfigStores, configStores...)
	return nil
}

func mcpSecurityOptions(ctx context.Context, configSource *meshconfig.ConfigSource) (grpc.DialOption, error) {
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

func (s *Server) mcpController(
	opts *mcp.Options,
	conn *grpc.ClientConn,
	reporter monitoring.Reporter,
	clients *[]*sink.Client,
	configStores *[]model.ConfigStoreCache) {
	clientNodeID := ""
	all := collections.Pilot.All()
	cols := make([]sink.CollectionOptions, 0, len(all))
	for _, c := range all {
		cols = append(cols, sink.CollectionOptions{Name: c.Name().String(), Incremental: false})
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
	*clients = append(*clients, mcpClient)
	*configStores = append(*configStores, mcpController)
}

func (s *Server) makeKubeConfigController(args *PilotArgs) (model.ConfigStoreCache, error) {
	configClient, err := controller.NewClient(args.Config.KubeConfig, "", collections.Pilot,
		args.Config.ControllerOptions.DomainSuffix, buildLedger(args.Config), args.Revision)
	if err != nil {
		return nil, multierror.Prefix(err, "failed to open a config client.")
	}

	return controller.NewController(configClient, args.Config.ControllerOptions), nil
}

func (s *Server) makeFileMonitor(fileDir string, configController model.ConfigStore) error {
	fileSnapshot := configmonitor.NewFileSnapshot(fileDir, collections.Pilot)
	fileMonitor := configmonitor.NewMonitor("file-monitor", configController, FilepathWalkInterval, fileSnapshot.ReadConfigFiles)

	// Defer starting the file monitor until after the service is created.
	s.addStartFunc(func(stop <-chan struct{}) error {
		fileMonitor.Start(stop)
		return nil
	})

	return nil
}

func grpcDial(ctx context.Context,
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
