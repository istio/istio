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

package xds

import (
	"bytes"
	"fmt"
	"io"



	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/util/protomarshal"
)

// BootstrapGenerator produces an Envoy bootstrap from node descriptors.
type BootstrapGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &BootstrapGenerator{}

// if enableFlag is "1" indicates that AcceptHttp_10 is enabled.
func enableHTTP10(enableFlag string) bool {
	return enableFlag == "1"
}

// Generate returns a bootstrap discovery response.
func (e *BootstrapGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	// The model.Proxy information is incomplete, re-parse the discovery request.
	node := bootstrap.ConvertXDSNodeToNode(proxy.XdsNode)

	var buf bytes.Buffer
	templateFile := bootstrap.GetEffectiveTemplatePath(node.Metadata.ProxyConfig)
	err := bootstrap.New(bootstrap.Config{
		Node: node,
	}).WriteTo(templateFile, io.Writer(&buf))
	if err != nil {
		return nil, model.DefaultXdsLogDetails, fmt.Errorf("failed to generate bootstrap config: %v", err)
	}

	bs := &bootstrapv3.Bootstrap{}
	if err = protomarshal.Unmarshal(buf.Bytes(), bs); err != nil {
		log.Warnf("failed to unmarshal bootstrap from JSON %q: %v", buf.String(), err)
	}
	if features.HTTP10 || enableHTTP10(proxy.Metadata.HTTP10) {
		log.Infof("http10 enabled")
		if bs.StaticResources != nil {
			for _, listener := range bs.StaticResources.Listeners {
				if listener != nil {
					for _, filterChain := range listener.FilterChains {
						if filterChain != nil {
							for _, filter := range filterChain.Filters {
								if filter != nil {
									if filter.Name == wellknown.HTTPConnectionManager {
										httpProxy := &hcm.HttpConnectionManager{}
										filter.GetTypedConfig().TypeUrl = "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"
										err = filter.GetTypedConfig().UnmarshalTo(httpProxy)
										if err != nil {
											log.Warnf("try to deal with bootstrap static resources for proxy[%s], failed to unmarshal filter [%s->%s] to hcm,errMsg: [%s]", proxy.ID, listener.GetAddress().String(), filter.Name, err.Error())
											continue
										}
										if httpProxy.HttpProtocolOptions != nil {
											httpProxy.HttpProtocolOptions.AcceptHttp_10 = true
										} else {
											httpProxy.HttpProtocolOptions = &envoycorev3.Http1ProtocolOptions{
												AcceptHttp_10: true,
											}
										}
										err = filter.GetTypedConfig().MarshalFrom(httpProxy)
										if err != nil {
											log.Warnf("after add http10 option, hcm filter failed to marshal from it ")
											continue
										} else {
											log.Infof("Due to enabled http10 for [%s], the filter [%s->%s] under bootstrap static resources be added http10 option successfully",
												proxy.ID, listener.GetAddress().String(), filter.GetName())
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	bs = e.applyPatches(bs, proxy, req.Push)
	return model.Resources{
		&discovery.Resource{
			Resource: protoconv.MessageToAny(bs),
		},
	}, model.DefaultXdsLogDetails, nil
}

func (e *BootstrapGenerator) applyPatches(bs *bootstrapv3.Bootstrap, proxy *model.Proxy, push *model.PushContext) *bootstrapv3.Bootstrap {
	patches := push.EnvoyFilters(proxy)
	if patches == nil {
		return bs
	}
	defer runtime.HandleCrash(runtime.LogPanic, func(any) {
		envoyfilter.IncrementEnvoyFilterErrorMetric(envoyfilter.Bootstrap)
		log.Errorf("bootstrap patch caused panic, so the patches did not take effect")
	})
	for _, patch := range patches.Patches[networking.EnvoyFilter_BOOTSTRAP] {
		if patch.Operation == networking.EnvoyFilter_Patch_MERGE {
			proto.Merge(bs, patch.Value)
			envoyfilter.IncrementEnvoyFilterMetric(patch.Key(), envoyfilter.Bootstrap, true)
		} else {
			envoyfilter.IncrementEnvoyFilterErrorMetric(envoyfilter.Bootstrap)
		}
	}
	return bs
}
