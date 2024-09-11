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

package driver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	extensionservice "github.com/envoyproxy/go-control-plane/envoy/service/extension/v3"
	secret "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"

	"istio.io/istio/tests/envoye2e/workloadapi"
)

// XDS creates an xDS server
type XDS struct {
	grpc *grpc.Server
}

// XDSServer is a struct holding xDS state.
type XDSServer struct {
	cache.MuxCache
	Extensions *ExtensionServer
	Cache      cache.SnapshotCache
	Workloads  *cache.LinearCache
}

var _ Step = &XDS{}

const WorkloadTypeURL = "type.googleapis.com/istio.workload.Workload"

// Run starts up an Envoy XDS server.
func (x *XDS) Run(p *Params) error {
	log.Printf("XDS server starting on %d\n", p.Ports.XDSPort)
	x.grpc = grpc.NewServer()
	p.Config.Extensions = NewExtensionServer(context.Background())
	extensionservice.RegisterExtensionConfigDiscoveryServiceServer(x.grpc, p.Config.Extensions)

	// Register caches.
	p.Config.Cache = cache.NewSnapshotCache(false, cache.IDHash{}, x)
	p.Config.Workloads = cache.NewLinearCache(WorkloadTypeURL,
		cache.WithLogger(x))

	p.Config.Caches = map[string]cache.Cache{
		"default":   p.Config.Cache,
		"workloads": p.Config.Workloads,
	}
	p.Config.Classify = func(r *cache.Request) string {
		if r.TypeUrl == WorkloadTypeURL {
			return "workloads"
		}
		return "default"
	}
	p.Config.ClassifyDelta = func(r *cache.DeltaRequest) string {
		if r.TypeUrl == WorkloadTypeURL {
			return "workloads"
		}
		return "default"
	}

	xdsServer := server.NewServer(context.Background(), &p.Config, nil)
	discovery.RegisterAggregatedDiscoveryServiceServer(x.grpc, xdsServer)
	secret.RegisterSecretDiscoveryServiceServer(x.grpc, xdsServer)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", p.Ports.XDSPort))
	if err != nil {
		return err
	}

	go func() {
		_ = x.grpc.Serve(lis)
	}()
	return nil
}

type NamedWorkload struct {
	*workloadapi.Workload
}

func (nw *NamedWorkload) GetName() string {
	return nw.Uid
}

var _ types.ResourceWithName = &NamedWorkload{}

// Cleanup stops the XDS server.
func (x *XDS) Cleanup() {
	log.Println("stopping XDS server")
	x.grpc.GracefulStop()
}

func (x *XDS) Debugf(format string, args ...interface{}) {
	log.Printf("xds debug: "+format, args...)
}

func (x *XDS) Infof(format string, args ...interface{}) {
	log.Printf("xds: "+format, args...)
}

func (x *XDS) Errorf(format string, args ...interface{}) {
	log.Printf("xds error: "+format, args...)
}

func (x *XDS) Warnf(format string, args ...interface{}) {
	log.Printf("xds warn: "+format, args...)
}

type Update struct {
	Node      string
	Version   string
	Listeners []string
	Clusters  []string
	Secrets   []string
}

var _ Step = &Update{}

func (u *Update) Run(p *Params) error {
	p.Vars["Version"] = u.Version
	version, err := p.Fill(u.Version)
	if err != nil {
		return err
	}
	log.Printf("update config for %q with version %q", u.Node, version)

	clusters := make([]types.Resource, 0, len(u.Clusters))
	for _, cluster := range u.Clusters {
		out := &clusterv3.Cluster{}
		if err := p.FillYAML(cluster, out); err != nil {
			return err
		}
		clusters = append(clusters, out)
	}

	listeners := make([]types.Resource, 0, len(u.Listeners))
	for _, listener := range u.Listeners {
		out := &listenerv3.Listener{}
		if err := p.FillYAML(listener, out); err != nil {
			return err
		}
		listeners = append(listeners, out)
	}

	secrets := make([]types.Resource, 0, len(u.Secrets))
	for _, secret := range u.Secrets {
		out := &tls.Secret{}
		if err := p.FillYAML(secret, out); err != nil {
			return err
		}
		secrets = append(secrets, out)
	}

	snap := &cache.Snapshot{}
	snap.Resources[types.Cluster] = cache.NewResources(version, clusters)
	snap.Resources[types.Listener] = cache.NewResources(version, listeners)
	snap.Resources[types.Secret] = cache.NewResources(version, secrets)
	return p.Config.Cache.SetSnapshot(context.Background(), u.Node, snap)
}

func (u *Update) Cleanup() {}

type UpdateExtensions struct {
	Extensions []string
}

var _ Step = &UpdateExtensions{}

func (u *UpdateExtensions) Run(p *Params) error {
	for _, extension := range u.Extensions {
		out := &core.TypedExtensionConfig{}
		if err := p.FillYAML(extension, out); err != nil {
			return err
		}
		log.Printf("updating extension config %q", out.Name)
		if err := p.Config.Extensions.Update(out); err != nil {
			return err
		}
	}
	return nil
}

func (u *UpdateExtensions) Cleanup() {}

type WorkloadMetadata struct {
	Address  string
	Metadata string
}

type UpdateWorkloadMetadata struct {
	Workloads []WorkloadMetadata
}

var _ Step = &UpdateWorkloadMetadata{}

func (u *UpdateWorkloadMetadata) Run(p *Params) error {
	for _, wl := range u.Workloads {
		out := &workloadapi.Workload{}
		if err := p.FillYAML(wl.Metadata, out); err != nil {
			return err
		}
		// Parse address as IP bytes
		ip, err := netip.ParseAddr(wl.Address)
		if err != nil {
			return err
		}
		log.Printf("updating metadata for %q\n", wl.Address)
		out.Addresses = [][]byte{ip.AsSlice()}
		namedWorkload := &NamedWorkload{out}
		err = p.Config.Workloads.UpdateResource(namedWorkload.GetName(), namedWorkload)
		if err != nil {
			return err
		}
	}
	return nil
}

func (u *UpdateWorkloadMetadata) Cleanup() {}
