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
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/test/util/assert"
)

func TestInitProxyMetadataGatesHBONEConnectWait(t *testing.T) {
	cases := []struct {
		name       string
		flag       bool
		extensions []string
		want       bool
	}{
		{name: "flag unset", extensions: util.HBONEConnectWaitExtensions.UnsortedList()},
		{name: "flag set, extensions reported", flag: true, extensions: util.HBONEConnectWaitExtensions.UnsortedList(), want: true},
		{name: "flag set, no extensions", flag: true},
		{name: "flag set, transport only", flag: true, extensions: []string{util.HBONEOriginationAdapterTransport}},
		{name: "flag set, pool only", flag: true, extensions: []string{util.HBONEConnectWaitPool}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fields := map[string]any{}
			if tt.flag {
				fields["ENABLE_HBONE_CONNECT_WAIT"] = "true"
			}
			meta, err := structpb.NewStruct(fields)
			assert.NoError(t, err)
			node := &core.Node{
				Id:       "sidecar~1.1.1.1~app.default~default.svc.cluster.local",
				Metadata: meta,
			}
			for _, name := range tt.extensions {
				node.Extensions = append(node.Extensions, &core.Extension{Name: name})
			}
			proxy, err := (&DiscoveryServer{}).initProxyMetadata(node)
			assert.NoError(t, err)
			assert.Equal(t, bool(proxy.Metadata.EnableHBONEConnectWait), tt.want)
		})
	}
}
