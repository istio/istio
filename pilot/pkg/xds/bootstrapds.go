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
	"github.com/golang/protobuf/jsonpb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/bootstrap"
)

// Bootstrap generator produces an Envoy bootstrap from node descriptors.
type BootstrapGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &BootstrapGenerator{}

// Generate returns a bootstrap discovery response.
func (e *BootstrapGenerator) Generate(proxy *model.Proxy, _ *model.PushContext, _ *model.WatchedResource,
	_ *model.PushRequest) (model.Resources, *model.XdsLogDetails, error) {
	// The model.Proxy information is incomplete, re-parse the discovery request.
	node := bootstrap.ConvertXDSNodeToNode(proxy.XdsNode)

	var buf bytes.Buffer
	templateFile := bootstrap.GetEffectiveTemplatePath(node.Metadata.ProxyConfig)
	err := bootstrap.New(bootstrap.Config{
		Node: node,
	}).WriteTo(templateFile, io.Writer(&buf))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate bootstrap config: %v", err)
	}

	bs := &bootstrapv3.Bootstrap{}
	if err = jsonpb.Unmarshal(io.Reader(&buf), bs); err != nil {
		log.Warnf("failed to unmarshal bootstrap from JSON %q: %v", buf.String(), err)
	}
	return model.Resources{
		&discovery.Resource{
			Resource: util.MessageToAny(bs),
		},
	}, nil, nil
}
