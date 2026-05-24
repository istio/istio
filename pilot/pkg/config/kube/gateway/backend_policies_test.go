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

package gateway

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	gw "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
)

func TestGetBackendTLSCredentialNameKeepsConfigMapResourceNameWhenReferenceIsMissing(t *testing.T) {
	conds := backendTLSConditionsForTest()
	ref := backendTLSConfigMapRef("root-cert")
	refs := gatewaycommon.NewReferenceSet()
	refs.ErasedCollections[gvk.ConfigMap] = func(krt.HandlerContext, string, string) (any, bool) {
		return nil, false
	}

	got := getBackendTLSCredentialName(nil, gw.BackendTLSPolicyValidation{
		CACertificateRefs: []gw.LocalObjectReference{ref},
	}, "backend-ns", conds, refs)

	assertEqual(t, got, credentials.KubernetesConfigMapTypeURI+"backend-ns/root-cert")
	assertConditionError(t, conds, string(gw.PolicyConditionAccepted))
	assertConditionError(t, conds, string(gw.BackendTLSPolicyConditionResolvedRefs))
}

func TestGetBackendTLSCredentialNameKeepsConfigMapResourceNameWhenCertificateIsInvalid(t *testing.T) {
	conds := backendTLSConditionsForTest()
	ref := backendTLSConfigMapRef("bad-root")
	refs := gatewaycommon.NewReferenceSet()
	refs.ErasedCollections[gvk.ConfigMap] = func(krt.HandlerContext, string, string) (any, bool) {
		return &v1.ConfigMap{Data: map[string]string{"unrelated": "value"}}, true
	}

	got := getBackendTLSCredentialName(nil, gw.BackendTLSPolicyValidation{
		CACertificateRefs: []gw.LocalObjectReference{ref},
	}, "backend-ns", conds, refs)

	assertEqual(t, got, credentials.KubernetesConfigMapTypeURI+"backend-ns/bad-root")
	assertConditionError(t, conds, string(gw.PolicyConditionAccepted))
	assertConditionError(t, conds, string(gw.BackendTLSPolicyConditionResolvedRefs))
}

func TestGetBackendTLSCredentialNameUsesInvalidResourceForUnsupportedReference(t *testing.T) {
	conds := backendTLSConditionsForTest()
	ref := gw.LocalObjectReference{
		Group: gw.Group(""),
		Kind:  gw.Kind("Secret"),
		Name:  gw.ObjectName("secret-root"),
	}
	refs := gatewaycommon.NewReferenceSet()
	refs.ErasedCollections[gvk.ConfigMap] = func(krt.HandlerContext, string, string) (any, bool) {
		return nil, false
	}

	got := getBackendTLSCredentialName(nil, gw.BackendTLSPolicyValidation{
		CACertificateRefs: []gw.LocalObjectReference{ref},
	}, "backend-ns", conds, refs)

	assertEqual(t, got, credentials.InvalidSecretTypeURI)
	assertConditionError(t, conds, string(gw.PolicyConditionAccepted))
	assertConditionError(t, conds, string(gw.BackendTLSPolicyConditionResolvedRefs))
}

func backendTLSConfigMapRef(name string) gw.LocalObjectReference {
	return gw.LocalObjectReference{
		Group: gw.Group(gvk.ConfigMap.Group),
		Kind:  gw.Kind(gvk.ConfigMap.Kind),
		Name:  gw.ObjectName(name),
	}
}

func backendTLSConditionsForTest() map[string]*condition {
	return map[string]*condition{
		string(gw.PolicyConditionAccepted): {
			reason:  string(gw.PolicyReasonAccepted),
			message: "Configuration is valid",
		},
		string(gw.BackendTLSPolicyConditionResolvedRefs): {
			reason:  string(gw.BackendTLSPolicyReasonResolvedRefs),
			message: "Configuration is valid",
		},
	}
}

func assertConditionError(t *testing.T, conds map[string]*condition, name string) {
	t.Helper()
	cond, ok := conds[name]
	if !ok {
		t.Fatalf("missing condition %q", name)
	}
	if cond.error == nil {
		t.Fatalf("condition %q: expected error, got nil", name)
	}
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
