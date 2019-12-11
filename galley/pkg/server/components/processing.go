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

	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	mcp "istio.io/api/mcp/v1alpha1"

	"istio.io/istio/galley/pkg/config/processor/groups"
	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/metadata"
	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/server/process"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/galley/pkg/source/kube/builtin"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	configz "istio.io/istio/pkg/mcp/configz/server"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	mcprate "istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
)

// Processing component.
type Processing struct {
	args *settings.Args

	distributor  *snapshot.Cache
	configzTopic fw.Topic

	serveWG       sync.WaitGroup
	grpcServer    *grpc.Server
	processor     *runtime.Processor
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
	d := snapshot.New(groups.IndexFunction)
	return &Processing{
		args:         a,
		distributor:  d,
		configzTopic: configz.CreateTopic(d),
	}
}

// Start implements process.Component
func (p *Processing) Start() (err error) {
	// TODO: cleanup

	var mesh meshconfig.Cache
	var src runtime.Source

	if mesh, err = newMeshConfigCache(p.args.MeshConfigFile); err != nil {
		return
	}

	if src, err = p.createSource(mesh); err != nil {
		return
	}

	types := p.getMCPTypes()

	processorCfg := runtime.Config{
		DomainSuffix:             p.args.DomainSuffix,
		Mesh:                     mesh,
		Schema:                   types,
		SynthesizeServiceEntries: p.args.EnableServiceDiscovery,
	}
	p.processor = runtime.NewProcessor(src, p.distributor, &processorCfg)

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
		Watcher:            p.distributor,
		Reporter:           p.reporter,
		CollectionsOptions: source.CollectionOptionsFromSlice(types.Collections()),
		ConnRateLimiter:    mcprate.NewRateLimiter(time.Second, 100), // TODO(Nino-K): https://github.com/istio/istio/issues/12074
	}

	md := grpcMetadata.MD{
		versionMetadataKey: []string{version.Info.Version},
	}
	if err := parseSinkMeta(p.args.SinkMeta, md); err != nil {
		return err
	}

	if p.args.SinkAddress != "" {
		p.callOut, err = newCallout(p.args.SinkAddress, p.args.SinkAuthMode, md, options)
		if err != nil {
			p.callOut = nil
			scope.Errorf("Callout could not be initialized: %v", err)
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
		err := p.processor.Start()
		if err != nil {
			scope.Errorf("Galley Server unexpectedly terminated: %v", err)
			return
		}

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

func (p *Processing) createSource(mesh meshconfig.Cache) (src runtime.Source, err error) {
	converterCfg := &converter.Config{
		Mesh:         mesh,
		DomainSuffix: p.args.DomainSuffix,
	}

	sourceSchema := p.getSourceSchema()

	if p.args.ConfigPath != "" {
		if src, err = fsNew(p.args.ConfigPath, sourceSchema, converterCfg, p.args.WatchConfigFiles); err != nil {
			return
		}
	} else {
		var k client.Interfaces
		if k, err = newKubeFromConfigFile(p.args.KubeConfig); err != nil {
			return
		}
		var found []schema.ResourceSpec

		if !p.args.DisableResourceReadyCheck {
			found, err = verifyResourceTypesPresence(k, sourceSchema.All())
		} else {
			found, err = findSupportedResources(k, sourceSchema.All())
		}
		if err != nil {
			return
		}
		sourceSchema = schema.New(found...)
		if src, err = newSource(k, p.args.ResyncPeriod, sourceSchema, converterCfg); err != nil {
			return
		}
	}
	return
}

func (p *Processing) getSourceSchema() *schema.Instance {
	b := schema.NewBuilder()
	for _, spec := range kubeMeta.Types.All() {

		if p.args.EnableServiceDiscovery {
			// TODO: IsBuiltIn is a proxy for types needed for service discovery
			if builtin.IsBuiltIn(spec.Kind) {
				b.Add(spec)
				continue
			}
		}

		if !p.isKindExcluded(spec.Kind) {
			b.Add(spec)
		}
	}
	return b.Build()
}

func (p *Processing) isKindExcluded(kind string) bool {
	for _, excludedKind := range p.args.ExcludedResourceKinds {
		if kind == excludedKind {
			return true
		}
	}
	return false
}

func (p *Processing) getMCPTypes() *resource.Schema {
	b := resource.NewSchemaBuilder()
	b.RegisterSchema(metadata.Types)
	b.Register(
		"istio/mesh/v1alpha1/MeshConfig",
		"type.googleapis.com/istio.mesh.v1alpha1.MeshConfig")

	for _, k := range p.args.ExcludedResourceKinds {
		spec := kubeMeta.Types.Get(k)
		if spec != nil {
			b.UnregisterInfo(spec.Target)
		}
	}

	return b.Build()
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

	if p.processor != nil {
		p.processor.Stop()
		p.processor = nil
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
