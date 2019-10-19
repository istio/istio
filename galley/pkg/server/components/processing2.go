// Copyright 2018 Istio Authors
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

package components

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	grpcMetadata "google.golang.org/grpc/metadata"

	mcp "istio.io/api/mcp/v1alpha1"

	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/groups"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/status"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/galley/pkg/server/process"
	"istio.io/istio/galley/pkg/server/settings"
	configz "istio.io/istio/pkg/mcp/configz/server"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	mcprate "istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
)

const versionMetadataKey = "config.source.version"

// Processing2 component is the main config processing component that will listen to a config source and publish
// resources through an MCP server, or a dialout connection.
type Processing2 struct {
	args *settings.Args

	mcpCache     *snapshot.Cache
	configzTopic fw.Topic

	k kube.Interfaces

	serveWG       sync.WaitGroup
	grpcServer    *grpc.Server
	runtime       *processing.Runtime
	mcpSource     *source.Server
	reporter      monitoring.Reporter
	callOut       *callout
	listenerMutex sync.Mutex
	listener      net.Listener
	stopCh        chan struct{}
}

var _ process.Component = &Processing2{}

// NewProcessing2 returns a new processing component.
func NewProcessing2(a *settings.Args) *Processing2 {
	mcpCache := snapshot.New(groups.IndexFunction)
	return &Processing2{
		args:         a,
		mcpCache:     mcpCache,
		configzTopic: configz.CreateTopic(mcpCache),
	}
}

// Start implements process.Component
func (p *Processing2) Start() (err error) {
	var mesh event.Source
	var src event.Source
	var updater snapshotter.StatusUpdater

	if mesh, err = meshcfgNewFS(p.args.MeshConfigFile); err != nil {
		return
	}

	m := metadata.MustGet()

	kubeResources := kuberesource.DisableExcludedKubeResources(m.KubeSource().Resources(), p.args.ExcludedResourceKinds, p.args.EnableServiceDiscovery)

	if src, updater, err = p.createSourceAndStatusUpdater(kubeResources); err != nil {
		return
	}

	transformProviders := transforms.Providers(m)

	var distributor snapshotter.Distributor = snapshotter.NewMCPDistributor(p.mcpCache)
	if p.args.EnableConfigAnalysis {
		settings := snapshotter.AnalyzingDistributorSettings{
			StatusUpdater:     updater,
			Analyzer:          analyzers.AllCombined().WithDisabled(kubeResources.DisabledCollections(), transformProviders),
			Distributor:       distributor,
			AnalysisSnapshots: []string{metadata.Default, metadata.SyntheticServiceEntry},
			TriggerSnapshot:   metadata.Default,
		}
		distributor = snapshotter.NewAnalyzingDistributor(settings)
	}

	processorSettings := processor.Settings{
		Metadata:           m,
		DomainSuffix:       p.args.DomainSuffix,
		Source:             event.CombineSources(mesh, src),
		TransformProviders: transformProviders,
		Distributor:        distributor,
		EnabledSnapshots:   []string{metadata.Default, metadata.SyntheticServiceEntry},
	}
	if p.runtime, err = processorInitialize(processorSettings); err != nil {
		return
	}

	grpcOptions := p.getServerGrpcOptions()

	p.stopCh = make(chan struct{})
	var checker source.AuthChecker = server.NewAllowAllChecker()
	if !p.args.Insecure {
		if checker, err = watchAccessList(p.stopCh, p.args.AccessListFile); err != nil {
			return
		}

		var watcher creds.CertificateWatcher
		if watcher, err = creds.PollFiles(p.stopCh, p.args.CredentialOptions); err != nil {
			return
		}
		credentials := creds.CreateForServer(watcher)

		grpcOptions = append(grpcOptions, grpc.Creds(credentials))
	}
	grpc.EnableTracing = p.args.EnableGRPCTracing
	p.grpcServer = grpc.NewServer(grpcOptions...)

	p.reporter = mcpMetricReporter("galley")

	options := &source.Options{
		Watcher:            p.mcpCache,
		Reporter:           p.reporter,
		CollectionsOptions: source.CollectionOptionsFromSlice(m.AllCollectionsInSnapshots()),
		ConnRateLimiter:    mcprate.NewRateLimiter(time.Second, 100), // TODO(Nino-K): https://github.com/istio/istio/issues/12074
	}

	md := grpcMetadata.MD{
		versionMetadataKey: []string{version.Info.Version},
	}
	if err = parseSinkMeta(p.args.SinkMeta, md); err != nil {
		return
	}

	if p.args.SinkAddress != "" {
		p.callOut, err = newCallout(p.args.SinkAddress, p.args.SinkAuthMode, md, options)
		if err != nil {
			p.callOut = nil
			err = fmt.Errorf("callout could not be initialized: %v", err)
			return
		}
	}

	serverOptions := &source.ServerOptions{
		AuthChecker: checker,
		RateLimiter: rate.NewLimiter(rate.Every(time.Second), 100), // TODO(Nino-K): https://github.com/istio/istio/issues/12074
		Metadata:    md,
	}

	p.mcpSource = source.NewServer(options, serverOptions)

	// get the network stuff setup
	network := "tcp"
	var address string
	idx := strings.Index(p.args.APIAddress, "://")
	if idx < 0 {
		address = p.args.APIAddress
	} else {
		network = p.args.APIAddress[:idx]
		address = p.args.APIAddress[idx+3:]
	}

	if p.listener, err = netListen(network, address); err != nil {
		err = fmt.Errorf("unable to listen: %v", err)
		return
	}

	mcp.RegisterResourceSourceServer(p.grpcServer, p.mcpSource)

	var startWG sync.WaitGroup
	startWG.Add(1)

	p.serveWG.Add(1)
	go func() {
		defer p.serveWG.Done()
		p.runtime.Start()

		l := p.getListener()
		if l != nil {
			// start serving
			gs := p.grpcServer
			startWG.Done()
			err = gs.Serve(l)
			if err != nil {
				scope.Errorf("Galley Server unexpectedly terminated: %v", err)
			}
		}
	}()

	if p.callOut != nil {
		p.serveWG.Add(1)
		go func() {
			defer p.serveWG.Done()
			p.callOut.run()
		}()
	}

	startWG.Wait()

	return nil
}

// ConfigZTopic returns the ConfigZTopic for the processor.
func (p *Processing2) ConfigZTopic() fw.Topic {
	return p.configzTopic
}

func (p *Processing2) getServerGrpcOptions() []grpc.ServerOption {
	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions,
		grpc.MaxConcurrentStreams(uint32(p.args.MaxConcurrentStreams)),
		grpc.MaxRecvMsgSize(int(p.args.MaxReceivedMessageSize)),
		grpc.InitialWindowSize(int32(p.args.InitialWindowSize)),
		grpc.InitialConnWindowSize(int32(p.args.InitialConnectionWindowSize)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Timeout:               p.args.KeepAlive.Timeout,
			Time:                  p.args.KeepAlive.Time,
			MaxConnectionAge:      p.args.KeepAlive.MaxServerConnectionAge,
			MaxConnectionAgeGrace: p.args.KeepAlive.MaxServerConnectionAgeGrace,
		}),
		// Relax keepalive enforcement policy requirements to avoid dropping connections due to too many pings.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	return grpcOptions
}

func (p *Processing2) getKubeInterfaces() (k kube.Interfaces, err error) {
	if p.k == nil {
		p.k, err = newInterfaces(p.args.KubeConfig)
	}
	k = p.k
	return
}

func (p *Processing2) createSourceAndStatusUpdater(resources schema.KubeResources) (
	src event.Source, updater snapshotter.StatusUpdater, err error) {

	if p.args.ConfigPath != "" {
		if src, err = fsNew2(p.args.ConfigPath, resources, p.args.WatchConfigFiles); err != nil {
			return
		}
		updater = &snapshotter.InMemoryStatusUpdater{}
	} else {
		var k kube.Interfaces
		if k, err = p.getKubeInterfaces(); err != nil {
			return
		}

		var statusCtl status.Controller
		if p.args.EnableConfigAnalysis {
			statusCtl = status.NewController("validationMessages")
		}

		o := apiserver.Options{
			Client:           k,
			ResyncPeriod:     p.args.ResyncPeriod,
			Resources:        resources,
			StatusController: statusCtl,
		}
		s := apiserver.New(o)
		src = s
		updater = s
	}
	return
}

// Stop implements process.Component
func (p *Processing2) Stop() {
	if p.stopCh != nil {
		close(p.stopCh)
		p.stopCh = nil
	}

	if p.grpcServer != nil {
		p.grpcServer.GracefulStop()
		p.grpcServer = nil
	}

	if p.runtime != nil {
		p.runtime.Stop()
		p.runtime = nil
	}

	p.listenerMutex.Lock()
	if p.listener != nil {
		_ = p.listener.Close()
		p.listener = nil
	}
	p.listenerMutex.Unlock()

	if p.reporter != nil {
		_ = p.reporter.Close()
		p.reporter = nil
	}

	if p.callOut != nil {
		p.callOut.stop()
		p.callOut = nil
	}

	if p.grpcServer != nil || p.callOut != nil {
		p.serveWG.Wait()
	}

	// final attempt to purge buffered logs
	_ = log.Sync()
}

func (p *Processing2) getListener() net.Listener {
	p.listenerMutex.Lock()
	defer p.listenerMutex.Unlock()
	return p.listener
}

// Address returns the Address of the MCP service.
func (p *Processing2) Address() net.Addr {
	l := p.getListener()
	if l == nil {
		return nil
	}
	return l.Addr()
}

func parseSinkMeta(pairs []string, md grpcMetadata.MD) error {
	for _, p := range pairs {
		kv := strings.Split(p, "=")
		if len(kv) != 2 || kv[0] == "" || kv[1] == "" {
			return fmt.Errorf("sinkMeta not in key=value format: %v", p)
		}
		md[kv[0]] = append(md[kv[0]], kv[1])
	}
	return nil
}
