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

package inject

import (
	"context"
	"errors"
	"testing"

	securityv1 "github.com/openshift/api/security/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
)

type fakeDynamicClient struct {
	kube.CLIClient
	dyn dynamic.Interface
}

func (f fakeDynamicClient) Dynamic() dynamic.Interface { return f.dyn }

func newTestSCCClient(t *testing.T, sccs ...*securityv1.SecurityContextConstraints) *SCCClient {
	t.Helper()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{sccGVR: "SecurityContextConstraintsList"})
	for _, scc := range sccs {
		scc.TypeMeta = metav1.TypeMeta{APIVersion: "security.openshift.io/v1", Kind: "SecurityContextConstraints"}
		u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(scc)
		if err != nil {
			t.Fatalf("failed to convert scc: %v", err)
		}
		if _, err := dyn.Resource(sccGVR).Create(context.Background(), &unstructured.Unstructured{Object: u}, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to create scc: %v", err)
		}
	}
	c := fakeDynamicClient{CLIClient: kube.NewFakeClient(), dyn: dyn}
	sc := NewSCCClient(c)
	stop := test.NewStop(t)
	c.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, sc.HasSynced)
	return sc
}

func TestGetSCCProxyIDs(t *testing.T) {
	// namespace's preallocated ranges: uid [1000,1009], gid [2000,2009]
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				securityv1.UIDRangeAnnotation:           "1000/10",
				securityv1.SupplementalGroupsAnnotation: "2000/10",
			},
		},
	}
	uidStrategy := func(typ securityv1.RunAsUserStrategyType, uid, uidRangeMin, uidRangeMax *int64) securityv1.RunAsUserStrategyOptions {
		return securityv1.RunAsUserStrategyOptions{Type: typ, UID: uid, UIDRangeMin: uidRangeMin, UIDRangeMax: uidRangeMax}
	}
	groupStrategy := func(typ securityv1.SupplementalGroupsStrategyType, ranges []securityv1.IDRange) securityv1.SupplementalGroupsStrategyOptions {
		return securityv1.SupplementalGroupsStrategyOptions{Type: typ, Ranges: ranges}
	}
	sccs := newTestSCCClient(t,
		&securityv1.SecurityContextConstraints{
			ObjectMeta:         metav1.ObjectMeta{Name: "no-explicit-range"},
			RunAsUser:          uidStrategy(securityv1.RunAsUserStrategyMustRunAsRange, nil, nil, nil),
			SupplementalGroups: groupStrategy(securityv1.SupplementalGroupsStrategyMustRunAs, nil),
		},
		&securityv1.SecurityContextConstraints{
			ObjectMeta:         metav1.ObjectMeta{Name: "superset-range"},
			RunAsUser:          uidStrategy(securityv1.RunAsUserStrategyMustRunAsRange, nil, ptr.Of(int64(0)), ptr.Of(int64(1000009))),
			SupplementalGroups: groupStrategy(securityv1.SupplementalGroupsStrategyMustRunAs, []securityv1.IDRange{{Min: 0, Max: 1000009}}),
		},
		&securityv1.SecurityContextConstraints{
			ObjectMeta:         metav1.ObjectMeta{Name: "custom-scc"},
			RunAsUser:          uidStrategy(securityv1.RunAsUserStrategyMustRunAsRange, nil, ptr.Of(int64(500000)), ptr.Of(int64(500009))),
			SupplementalGroups: groupStrategy(securityv1.SupplementalGroupsStrategyMustRunAs, []securityv1.IDRange{{Min: 500000, Max: 500009}}),
		},
		&securityv1.SecurityContextConstraints{
			ObjectMeta:         metav1.ObjectMeta{Name: "uid-single"},
			RunAsUser:          uidStrategy(securityv1.RunAsUserStrategyMustRunAs, ptr.Of(int64(1000)), nil, nil),
			SupplementalGroups: groupStrategy(securityv1.SupplementalGroupsStrategyMustRunAs, []securityv1.IDRange{{Min: 500000, Max: 500009}}),
		},
		&securityv1.SecurityContextConstraints{
			ObjectMeta:         metav1.ObjectMeta{Name: "group-any"},
			RunAsUser:          uidStrategy(securityv1.RunAsUserStrategyMustRunAsRange, nil, ptr.Of(int64(500000)), ptr.Of(int64(500009))),
			SupplementalGroups: groupStrategy(securityv1.SupplementalGroupsStrategyRunAsAny, nil),
		},
		&securityv1.SecurityContextConstraints{
			ObjectMeta:         metav1.ObjectMeta{Name: "unusable"},
			RunAsUser:          uidStrategy(securityv1.RunAsUserStrategyMustRunAsNonRoot, nil, nil, nil),
			SupplementalGroups: groupStrategy(securityv1.SupplementalGroupsStrategyMustRunAs, nil),
		},
		&securityv1.SecurityContextConstraints{
			ObjectMeta:         metav1.ObjectMeta{Name: "single-uid-range"},
			RunAsUser:          uidStrategy(securityv1.RunAsUserStrategyMustRunAsRange, nil, ptr.Of(int64(500000)), ptr.Of(int64(500000))),
			SupplementalGroups: groupStrategy(securityv1.SupplementalGroupsStrategyRunAsAny, nil),
		},
	)

	cases := []struct {
		name       string
		client     *SCCClient
		ns         *corev1.Namespace
		annotation string
		wantUID    *int64
		wantGID    *int64
	}{
		{"nil client", nil, ns, "custom-scc", nil, nil},
		{"no annotation", sccs, ns, "", nil, nil},
		{"unknown custom scc", sccs, ns, "does-not-exist", nil, nil},
		{"no explicit range defers to namespace", sccs, ns, "no-explicit-range", nil, nil},
		{"namespace range is a subset, defers to namespace", sccs, ns, "superset-range", nil, nil},
		{"namespace range not a subset, uses scc range", sccs, ns, "custom-scc", ptr.Of(int64(500009)), ptr.Of(int64(500009))},
		{"no namespace, uses scc range", sccs, nil, "custom-scc", ptr.Of(int64(500009)), ptr.Of(int64(500009))},
		{"single uid unusable, group not a subset uses scc", sccs, ns, "uid-single", nil, ptr.Of(int64(500009))},
		{"uid not a subset uses scc, group runasany", sccs, ns, "group-any", ptr.Of(int64(500009)), nil},
		{"neither resolves", sccs, ns, "unusable", nil, nil},
		{"single uid range refuses to assign proxy uid", sccs, ns, "single-uid-range", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{}
			if tc.annotation != "" {
				pod.Annotations = map[string]string{securityv1.ValidatedSCCAnnotation: tc.annotation}
			}
			uid, gid := GetSCCProxyIDs(tc.client, tc.ns, pod)
			if (uid == nil) != (tc.wantUID == nil) || (uid != nil && *uid != *tc.wantUID) {
				t.Errorf("uid: got %v, want %v", uid, tc.wantUID)
			}
			if (gid == nil) != (tc.wantGID == nil) || (gid != nil && *gid != *tc.wantGID) {
				t.Errorf("gid: got %v, want %v", gid, tc.wantGID)
			}
		})
	}
}

func TestNewSCCClient(t *testing.T) {
	cases := []struct {
		name         string
		probeErr     error
		wantInformer bool
	}{
		{"resource available", nil, true},
		{"resource not registered", apierrors.NewNotFound(securityv1.Resource(sccGVR.Resource), ""), false},
		{
			"rbac denied",
			apierrors.NewForbidden(securityv1.Resource(sccGVR.Resource), "", errors.New("user \"istiod\" cannot list securitycontextconstraints")), false,
		},
		{"transient probe error", errors.New("dial tcp: i/o timeout"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
				map[schema.GroupVersionResource]string{sccGVR: "SecurityContextConstraintsList"})
			if tc.probeErr != nil {
				// Fail only the constructor-time probe (first list); the informer's list must succeed.
				lists := 0
				dyn.PrependReactor("list", sccGVR.Resource, func(k8stesting.Action) (bool, runtime.Object, error) {
					lists++
					if lists == 1 {
						return true, nil, tc.probeErr
					}
					return false, nil, nil
				})
			}
			c := fakeDynamicClient{CLIClient: kube.NewFakeClient(), dyn: dyn}
			sc := NewSCCClient(c)
			if (sc.informer != nil) != tc.wantInformer {
				t.Fatalf("informer created = %v, want %v", sc.informer != nil, tc.wantInformer)
			}
			stop := test.NewStop(t)
			c.RunAndWait(stop)
			kube.WaitForCacheSync("test", stop, sc.HasSynced)

			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{securityv1.ValidatedSCCAnnotation: "anything"}}}
			if uid, gid := GetSCCProxyIDs(sc, nil, pod); uid != nil || gid != nil {
				t.Errorf("expected nil uid/gid for an unknown scc name, got uid=%v gid=%v", uid, gid)
			}
		})
	}
}
