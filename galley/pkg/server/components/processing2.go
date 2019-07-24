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

	"istio.io/istio/galley/pkg/config/analysis/analyzers"
	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/processing"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/source/kube"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/runtime/groups"
	"istio.io/istio/galley/pkg/server/process"
	"istio.io/istio/galley/pkg/server/settings"
	configz "istio.io/istio/pkg/mcp/configz/server"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	mcprate "istio.io/istio/pkg/mcp/rate"
	"istio.io/istio/pkg/mcp/server"
	"istio.io/istio/pkg/mcp/snapshot"
	"istio.io/istio/pkg/mcp/source"
	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
logic:
	// - Builtin types are excluded by default.
	// - If ServiceDiscovery is enabled, any built-in type should be readded.

	var result schema.KubeResources
	for _, r := range m.KubeSource().Resources() {

		if p.isKindExcluded(r.Kind) {
			// Found a matching exclude directive for this KubeResource. Disable the resource.
			r.Disabled = true

			// Check and see if this is needed for Service Discovery. If needed, we will need to re-enable.
			if p.args.EnableServiceDiscovery {
				// IsBuiltIn is a proxy for types needed for service discovery
				a := rt.DefaultProvider().GetAdapter(r)
				if a.IsBuiltIn() {
					// This is needed for service discovery. Re-enable.
					r.Disabled = false
				}
			}
		}

		result = append(result, r)
	}

	return result
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
		p.k, err = newKubeFromConfigFile(p.args.KubeConfig)
	}
	k = p.k
	return
}

func (p *Processing2) createSourceAndStatusUpdater(resources schema.KubeResources) (
	src event.Source, updater snapshotter.StatusUpdater, err error) {

	if p.args.ConfigPath != "" {
		if src, err = fsNew2(p.args.ConfigPath, resources); err != nil {
			return
		}
		updater = &snapshotter.InMemoryStatusUpdater{}
	} else {
		var k kube.Interfaces
		if k, err = p.getKubeInterfaces(); err != nil {
			return
		}

		if !p.args.DisableResourceReadyCheck {
			if err = checkResourceTypesPresence(k, resources); err != nil {
				return
			}
		}

		o := apiserver.Options{
			Client:       k,
			ResyncPeriod: p.args.ResyncPeriod,
			Resources:    resources,
		}
		s := apiserver.New(o)
		src = s
		updater = s
	}
	return
}

func (p *Processing2) isKindExcluded(kind string) bool {
	for _, excludedKind := range p.args.ExcludedResourceKinds {
		if kind == excludedKind {
			return true
		}
	}

	return false
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
