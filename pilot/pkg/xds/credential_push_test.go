// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xds

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

// TestCredentialUpdatePushes verifies that credential updates from the credentials controller
// (Secrets and CA-bearing ConfigMaps) only trigger SDS pushes:
// - Credentials are referenced by SDS resource name, so CDS, LDS, RDS, EDS and NDS are skipped
// - A credential update merged with another config change still pushes what that change needs
func TestCredentialUpdatePushes(t *testing.T) {
	type pushes struct {
		CDS, LDS, RDS, EDS, NDS, SDS bool
	}

	tests := []struct {
		name          string
		proxyType     model.NodeType
		reason        model.ReasonStats
		configUpdates sets.Set[model.ConfigKey]
		want          pushes
	}{
		{
			name:      "Sidecar with Secret update should only push SDS",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.SecretTrigger),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.Secret, Name: "my-secret", Namespace: "default"},
			),
			want: pushes{CDS: false, LDS: false, RDS: false, EDS: false, NDS: false, SDS: true},
		},
		{
			name:      "Sidecar with ConfigMap update should only push SDS",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.SecretTrigger),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ConfigMap, Name: "kube-root-ca.crt", Namespace: "default"},
			),
			want: pushes{CDS: false, LDS: false, RDS: false, EDS: false, NDS: false, SDS: true},
		},
		{
			name:      "Router with Secret update should only push SDS",
			proxyType: model.Router,
			reason:    model.NewReasonStats(model.SecretTrigger),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.Secret, Name: "my-secret", Namespace: "default"},
			),
			want: pushes{CDS: false, LDS: false, RDS: false, EDS: false, NDS: false, SDS: true},
		},
		{
			name:      "Router with ConfigMap update should only push SDS",
			proxyType: model.Router,
			reason:    model.NewReasonStats(model.SecretTrigger),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ConfigMap, Name: "kube-root-ca.crt", Namespace: "default"},
			),
			want: pushes{CDS: false, LDS: false, RDS: false, EDS: false, NDS: false, SDS: true},
		},
		{
			name:      "Waypoint with Secret update should only push SDS",
			proxyType: model.Waypoint,
			reason:    model.NewReasonStats(model.SecretTrigger),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.Secret, Name: "my-secret", Namespace: "default"},
			),
			want: pushes{CDS: false, LDS: false, RDS: false, EDS: false, NDS: false, SDS: true},
		},
		{
			name:      "Waypoint with ConfigMap update should only push SDS",
			proxyType: model.Waypoint,
			reason:    model.NewReasonStats(model.SecretTrigger),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ConfigMap, Name: "kube-root-ca.crt", Namespace: "default"},
			),
			want: pushes{CDS: false, LDS: false, RDS: false, EDS: false, NDS: false, SDS: true},
		},
		{
			name:      "Sidecar with ConfigMap and VirtualService updates should still push CDS, LDS and RDS",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.SecretTrigger, model.ConfigUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ConfigMap, Name: "kube-root-ca.crt", Namespace: "default"},
				model.ConfigKey{Kind: kind.VirtualService, Name: "my-vs", Namespace: "default"},
			),
			// SDS is still pushed for the ConfigMap
			want: pushes{CDS: true, LDS: true, RDS: true, EDS: false, NDS: false, SDS: true},
		},
		{
			name:      "Sidecar with ConfigMap and ServiceEntry updates should push all",
			proxyType: model.SidecarProxy,
			reason:    model.NewReasonStats(model.SecretTrigger, model.ServiceUpdate),
			configUpdates: sets.New(
				model.ConfigKey{Kind: kind.ConfigMap, Name: "kube-root-ca.crt", Namespace: "default"},
				model.ConfigKey{Kind: kind.ServiceEntry, Name: "my-svc.default.svc.cluster.local", Namespace: "default"},
			),
			want: pushes{CDS: true, LDS: true, RDS: true, EDS: true, NDS: true, SDS: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &model.Proxy{
				Type:              tt.proxyType,
				IPAddresses:       []string{"1.1.1.1"},
				ID:                "test-proxy.default",
				Metadata:          &model.NodeMetadata{},
				MergedGateway:     &model.MergedGateway{},
				PrevMergedGateway: &model.PrevMergedGateway{},
			}

			req := &model.PushRequest{
				ConfigsUpdated: tt.configUpdates,
				Reason:         tt.reason,
				Push:           &model.PushContext{},
			}

			_, cds := cdsNeedsPush(req, proxy)
			got := pushes{
				CDS: cds,
				LDS: ldsNeedsPush(proxy, req),
				RDS: RdsNeedsPush(req, proxy),
				EDS: edsNeedsPush(req, proxy),
				NDS: ndsNeedsPush(req, proxy),
				SDS: sdsNeedsPush(req.Forced, req.ConfigsUpdated),
			}
			assert.Equal(t, got, tt.want)
		})
	}
}
