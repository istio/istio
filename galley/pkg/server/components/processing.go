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
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/processor/groups"
	"istio.io/istio/galley/pkg/config/processor/transforms"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/status"
	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/galley/pkg/envvar"
	"istio.io/istio/galley/pkg/server/process"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/snapshots"
	configz "istio.io/istio/pkg/mcp/configz/server"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	mcprate "istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
)

const versionMetadataKey = "config.source.version"

// Processing component is the main config processing component that will listen to a config source and publish
// resources through an MCP server, or a dialout connection.
type Processing struct {
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

var _ process.Component = &Processing{}

// NewProcessing returns a new processing component.
func NewProcessing(a *settings.Args) *Processing {
	mcpCache := snapshot.New(groups.IndexFunction)
	return &Processing{
		args:         a,
		mcpCache:     mcpCache,
		configzTopic: configz.CreateTopic(mcpCache),
	}
}

// Start implements process.Component
func (p *Processing) Start() (err error) {
	var mesh event.Source
	var src event.Source
	var updater snapshotter.StatusUpdater

	if mesh, err = meshcfgNewFS(p.args.MeshConfigFile); err != nil {
		return
	}

	m := schema.MustGet()

	transformProviders := transforms.Providers(m)

	// Disable any unnecessary resources, including resources not in configured snapshots
	var colsInSnapshots collection.Names
	for _, c := range m.AllCollectionsInSnapshots(p.args.Snapshots) {
		colsInSnapshots = append(colsInSnapshots, collection.NewName(c))
	}
	kubeResources := kuberesource.DisableExcludedCollections(m.KubeCollections(), transformProviders,
		colsInSnapshots, p.args.ExcludedResourceKinds, p.args.EnableServiceDiscovery)

	if src, updater, err = p.createSourceAndStatusUpdater(kubeResources); err != nil {
		return
	}

	var distributor snapshotter.Distributor = snapshotter.NewMCPDistributor(p.mcpCache)

	if p.args.EnableConfigAnalysis {
		combinedAnalyzer := analyzers.AllCombined()
		combinedAnalyzer.RemoveSkipped(colsInSnapshots, kubeResources.DisabledCollectionNames(), transformProviders)

		distributor = snapshotter.NewAnalyzingDistributor(snapshotter.AnalyzingDistributorSettings{
			StatusUpdater:     updater,
			Analyzer:          combinedAnalyzer,
			Distributor:       distributor,
			AnalysisSnapshots: p.args.Snapshots,
			TriggerSnapshot:   p.args.TriggerSnapshot,
		})
	}

	processorSettings := processor.Settings{
		Metadata:           m,
		DomainSuffix:       p.args.DomainSuffix,
		Source:             event.CombineSources(mesh, src),
		TransformProviders: transformProviders,
		Distributor:        distributor,
		EnabledSnapshots:   p.args.Snapshots,
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

	mcpSourceRateLimiter := mcprate.NewRateLimiter(envvar.MCPSourceReqFreq.Get(), envvar.MCPSourceReqBurstSize.Get())
	options := &source.Options{
		Watcher:            p.mcpCache,
		Reporter:           p.reporter,
		CollectionsOptions: source.CollectionOptionsFromSlice(m.AllCollectionsInSnapshots(snapshots.SnapshotNames())),
		ConnRateLimiter:    mcpSourceRateLimiter,
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

	sourceServerRateLimiter := rate.NewLimiter(rate.Every(envvar.SourceServerStreamFreq.Get()), envvar.SourceServerStreamBurstSize.Get())
	serverOptions := &source.ServerOptions{
		AuthChecker: checker,
		RateLimiter: sourceServerRateLimiter,
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
func (p *Processing) ConfigZTopic() fw.Topic {
	return p.configzTopic
}

func (p *Processing) getServerGrpcOptions() []grpc.ServerOption {
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

func (p *Processing) getKubeInterfaces() (k kube.Interfaces, err error) {
	if p.args.KubeRestConfig != nil {
		return kube.NewInterfaces(p.args.KubeRestConfig), nil
	}
	if p.k == nil {
		p.k, err = newInterfaces(p.args.KubeConfig)
	}
	k = p.k
	return
}

func (p *Processing) createSourceAndStatusUpdater(schemas collection.Schemas) (
	src event.Source, updater snapshotter.StatusUpdater, err error) {

	if p.args.ConfigPath != "" {
		if src, err = fsNew(p.args.ConfigPath, schemas, p.args.WatchConfigFiles); err != nil {
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
			Schemas:          schemas,
			StatusController: statusCtl,
		}
		s := apiserver.New(o)
		src = s
		updater = s
	}
	return
}

// Stop implements process.Component
func (p *Processing) Stop() {
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

func (p *Processing) getListener() net.Listener {
	p.listenerMutex.Lock()
	defer p.listenerMutex.Unlock()
	return p.listener
}

// Address returns the Address of the MCP service.
func (p *Processing) Address() net.Addr {
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
