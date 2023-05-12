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

package ca

import (
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
)

type pod struct {
	name, namespace, account, node, uid string
}

func (p pod) Identity() string {
	return spiffe.Identity{
		TrustDomain:    "cluster.local",
		Namespace:      p.namespace,
		ServiceAccount: p.account,
	}.String()
}

func TestNodeAuthorizer(t *testing.T) {
	allowZtunnel := map[types.NamespacedName]struct{}{
		{Name: "ztunnel", Namespace: "istio-system"}: {},
	}
	ztunnelCaller := security.KubernetesInfo{
		PodName:           "ztunnel-a",
		PodNamespace:      "istio-system",
		PodUID:            "12345",
		PodServiceAccount: "ztunnel",
	}
	ztunnelPod := pod{
		name:      ztunnelCaller.PodName,
		namespace: ztunnelCaller.PodNamespace,
		account:   ztunnelCaller.PodServiceAccount,
		uid:       ztunnelCaller.PodUID,
		node:      "zt-node",
	}
	podSameNode := pod{
		name:      "pod-a",
		namespace: "ns-a",
		account:   "sa-a",
		uid:       "1",
		node:      "zt-node",
	}
	podOtherNode := pod{
		name:      "pod-b",
		namespace: podSameNode.namespace,
		account:   podSameNode.account,
		uid:       "2",
		node:      "other-node",
	}
	cases := []struct {
		name                    string
		pods                    []pod
		caller                  security.KubernetesInfo
		requestedIdentityString string
		trustedAccounts         map[types.NamespacedName]struct{}
		wantErr                 string
	}{
		{
			name:    "empty allowed identities",
			wantErr: "not allowed to impersonate",
		},
		{
			name:                    "allowed identities, but not on node",
			caller:                  ztunnelCaller,
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: podSameNode.Identity(),
			pods:                    []pod{ztunnelPod},
			wantErr:                 "no instances",
		},
		{
			name:                    "allowed identities, on node",
			caller:                  ztunnelCaller,
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: podSameNode.Identity(),
			pods:                    []pod{ztunnelPod, podSameNode},
			wantErr:                 "",
		},
		{
			name:                    "allowed identities, off node",
			caller:                  ztunnelCaller,
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: podSameNode.Identity(),
			pods:                    []pod{ztunnelPod, podOtherNode},
			wantErr:                 "no instances",
		},
		{
			name:                    "allowed identities, on and off node",
			caller:                  ztunnelCaller,
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: podSameNode.Identity(),
			pods:                    []pod{ztunnelPod, podSameNode, podOtherNode},
			wantErr:                 "",
		},
		{
			name:                    "invalid requested",
			caller:                  ztunnelCaller,
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: "not-spiffe-idenditity",
			pods:                    []pod{ztunnelPod},
			wantErr:                 "failed to validate impersonated identity",
		},
		{
			name:                    "unknown caller",
			caller:                  ztunnelCaller,
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: podSameNode.Identity(),
			pods:                    []pod{podSameNode},
			wantErr:                 "pod istio-system/ztunnel-a not found",
		},
		{
			name: "bad UID",
			caller: func(k security.KubernetesInfo) security.KubernetesInfo {
				k.PodUID = "bogus"
				return k
			}(ztunnelCaller),
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: podSameNode.Identity(),
			pods:                    []pod{ztunnelPod},
			wantErr:                 "pod found, but UID does not match",
		},
		{
			name:                    "bad account",
			caller:                  ztunnelCaller,
			trustedAccounts:         allowZtunnel,
			requestedIdentityString: podSameNode.Identity(),
			pods: []pod{func(p pod) pod {
				p.account = "bogus"
				return p
			}(ztunnelPod)},
			wantErr: "pod found, but ServiceAccount does not match",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var pods []runtime.Object
			for _, p := range tt.pods {
				pods = append(pods, &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      p.name,
						Namespace: p.namespace,
						UID:       types.UID(p.uid),
					},
					Spec: v1.PodSpec{
						ServiceAccountName: p.account,
						NodeName:           p.node,
					},
				})
			}
			c := kube.NewFakeClient(pods...)
			na, err := NewNodeAuthorizer(c, nil, tt.trustedAccounts)
			if err != nil {
				t.Fatal(err)
			}
			c.RunAndWait(test.NewStop(t))
			kube.WaitForCacheSync("test", test.NewStop(t), na.pods.HasSynced)

			err = na.authenticateImpersonation(tt.caller, tt.requestedIdentityString)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("wanted no error, got %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err)
			}
		})
	}
}
