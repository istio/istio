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
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/util/runtime"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/util/protomarshal"
)

// Bootstrap generator produces an Envoy bootstrap from node descriptors.
type BootstrapGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &BootstrapGenerator{}

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
