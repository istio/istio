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
package xds_test

import (
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

func TestECDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "./testdata/ecds.yaml"),
	})

	ads := s.ConnectADS().WithType(v3.ExtensionConfigurationType)
	wantExtensionConfigName := "extension-config"
	res := ads.RequestResponseAck(&discovery.DiscoveryRequest{
		Node: &corev3.Node{
			Id: ads.ID,
		},
		ResourceNames: []string{wantExtensionConfigName},
	})

	var ec corev3.TypedExtensionConfig
	err := ptypes.UnmarshalAny(res.Resources[0], &ec)
	if err != nil {
		t.Fatal("Failed to unmarshal extension config", err)
		return
	}
	if ec.Name != wantExtensionConfigName {
		t.Errorf("extension config name got %v want %v", ec.Name, wantExtensionConfigName)
	}
}
