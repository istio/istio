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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestCDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	ads := s.ConnectADS().WithType(v3.ClusterType)
	ads.RequestResponseAck(t, nil)
}

func TestCdsNeedsPush(t *testing.T) {
	testCases := []struct {
		name           string
		proxyType      model.NodeType
		configsUpdated map[model.ConfigKey]struct{}
		expected       bool
	}{
		{
			name:      "sidecar without configs that do not influence cds",
			proxyType: model.SidecarProxy,
			configsUpdated: map[model.ConfigKey]struct{}{
				{
					Kind:      gvk.VirtualService,
					Name:      "vs1",
					Namespace: "test",
				}: {},
				{
					Kind:      gvk.Gateway,
					Name:      "gw1",
					Namespace: "test",
				}: {},
				{
					Kind:      gvk.AuthorizationPolicy,
					Name:      "authz1",
					Namespace: "test",
				}: {},
				{
					Kind:      gvk.RequestAuthentication,
					Name:      "ra1",
					Namespace: "test",
				}: {},
			},
			expected: false,
		},
		{
			name:      "sidecar with configs that influence cds",
			proxyType: model.SidecarProxy,
			configsUpdated: map[model.ConfigKey]struct{}{
				{
					Kind:      gvk.VirtualService,
					Name:      "vs1",
					Namespace: "test",
				}: {},
				{
					Kind:      gvk.Gateway,
					Name:      "gw1",
					Namespace: "test",
				}: {},
				{
					Kind:      gvk.AuthorizationPolicy,
					Name:      "authz1",
					Namespace: "test",
				}: {},
				{
					Kind:      gvk.RequestAuthentication,
					Name:      "ra1",
					Namespace: "test",
				}: {},
				{
					Kind:      gvk.DestinationRule,
					Name:      "ra1",
					Namespace: "test",
				}: {},
			},
			expected: true,
		},
		{
			name:      "gateway with nil configs",
			proxyType: model.SidecarProxy,
			expected:  true,
		},
		{
			name:      "gateway with common configs that influence cds",
			proxyType: model.Router,
			configsUpdated: map[model.ConfigKey]struct{}{
				{
					Kind:      gvk.VirtualService,
					Name:      "vs1",
					Namespace: "test",
				}: {},
			},
			expected: true,
		},
		{
			name:      "gateway with common configs that influence cds",
			proxyType: model.Router,
			configsUpdated: map[model.ConfigKey]struct{}{
				{
					Kind:      gvk.ServiceEntry,
					Name:      "svc",
					Namespace: "test",
				}: {},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &model.PushRequest{
				Full:           true,
				ConfigsUpdated: tc.configsUpdated,
			}
			proxy := &model.Proxy{Type: tc.proxyType}
			got := xds.CdsNeedsPush(req, proxy)
			if got != tc.expected {
				t.Errorf("Got unexpected %v, expected %v", got, tc.expected)
			}
		})
	}
}
