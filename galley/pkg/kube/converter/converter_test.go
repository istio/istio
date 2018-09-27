//  Copyright 2018 Istio Authors
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

package converter

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/galley/pkg/kube/converter/legacy"
	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestGet(t *testing.T) {
	for name := range converters {
		// Should not panic
		_ = Get(name)
	}
}

func TestGet_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Should have panicked")
		}
	}()

	_ = Get("zzzzz")
}

var fakeCreateTime, _ = time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")

func TestIdentity(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Struct")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
			},
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := "key"

	outkey, createTime, pb, err := identity(info, key, u)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if key != outkey {
		t.Fatalf("Keys mismatch. Wanted=%s, Got=%s", key, outkey)
	}

	if !createTime.Equal(fakeCreateTime) {
		t.Fatalf("createTime mismatch: got %q want %q",
			createTime, fakeCreateTime)
	}

	actual, ok := pb.(*types.Struct)
	if !ok {
		t.Fatalf("Unable to convert to struct: %v", pb)
	}

	expected := &types.Struct{
		Fields: map[string]*types.Value{
			"foo": {
				Kind: &types.Value_StringValue{
					StringValue: "bar",
				},
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestIdentity_Error(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Empty")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := "key"

	_, _, _, err := identity(info, key, u)
	if err == nil {
		t.Fatal("Expected error not found")
	}
}

func TestLegacyMixerResource(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Struct")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"metadata": map[string]interface{}{
				"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
			},
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := "key"

	outkey, createTime, pb, err := legacyMixerResource(info, key, u)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedKey := "k1/" + key
	if outkey != expectedKey {
		t.Fatalf("Keys mismatch. Wanted=%s, Got=%s", expectedKey, outkey)
	}

	if !createTime.Equal(fakeCreateTime) {
		t.Fatalf("createTime mismatch: got %q want %q",
			createTime, fakeCreateTime)
	}

	actual, ok := pb.(*legacy.LegacyMixerResource)
	if !ok {
		t.Fatalf("Unable to convert to legacy: %v", pb)
	}

	expected := &legacy.LegacyMixerResource{
		Name: "key",
		Kind: "k1",
		Contents: &types.Struct{
			Fields: map[string]*types.Value{
				"foo": {
					Kind: &types.Value_StringValue{
						StringValue: "bar",
					},
				},
			},
		},
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", actual, expected)
	}
}

func TestLegacyMixerResource_Error(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Any")
	s := b.Build()

	info := s.Get("type.googleapis.com/google.protobuf.Any")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"spec": 23,
		},
	}

	key := "key"

	_, _, _, err := legacyMixerResource(info, key, u)
	if err == nil {
		t.Fatalf("expected error not found")
	}
}

func TestAuthPolicyResource(t *testing.T) {
	typeURL := fmt.Sprintf("type.googleapis.com/" + proto.MessageName((*authn.Policy)(nil)))
	b := resource.NewSchemaBuilder()
	b.Register(typeURL)
	s := b.Build()

	info := s.Get(typeURL)

	cases := []struct {
		name      string
		in        *unstructured.Unstructured
		wantProto *authn.Policy
	}{
		{
			name: "no-op",
			in: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Policy",
					"metadata": map[string]interface{}{
						"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
						"name":              "foo",
						"namespace":         "default",
					},
					"spec": map[string]interface{}{
						"targets": []interface{}{
							map[string]interface{}{
								"name": "foo",
							},
						},
						"peers": []interface{}{
							map[string]interface{}{
								"mtls": map[string]interface{}{},
							},
						},
					},
				},
			},
			wantProto: &authn.Policy{
				Targets: []*authn.TargetSelector{{
					Name: "foo",
				}},
				Peers: []*authn.PeerAuthenticationMethod{{
					&authn.PeerAuthenticationMethod_Mtls{&authn.MutualTls{}},
				}},
			},
		},
		{
			name: "partial nil peer method oneof",
			in: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Policy",
					"metadata": map[string]interface{}{
						"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
						"name":              "foo",
						"namespace":         "default",
					},
					"spec": map[string]interface{}{
						"targets": []interface{}{
							map[string]interface{}{
								"name": "foo",
							},
						},
						"peers": []interface{}{
							map[string]interface{}{
								"mtls": nil,
							},
						},
					},
				},
			},
			wantProto: &authn.Policy{
				Targets: []*authn.TargetSelector{{
					Name: "foo",
				}},
				Peers: []*authn.PeerAuthenticationMethod{{
					&authn.PeerAuthenticationMethod_Mtls{&authn.MutualTls{}},
				}},
			},
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(tt *testing.T) {
			wantKey, err := cache.MetaNamespaceKeyFunc(c.in)
			if err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}
			gotKey, createTime, pb, err := authPolicyResource(info, wantKey, c.in)
			if err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}

			if gotKey != wantKey {
				tt.Fatalf("Keys mismatch. got=%s, want=%s", gotKey, wantKey)
			}

			if !createTime.Equal(fakeCreateTime) {
				tt.Fatalf("createTime mismatch: got %q want %q",
					createTime, fakeCreateTime)
			}

			gotProto, ok := pb.(*authn.Policy)
			if !ok {
				tt.Fatalf("Unable to convert to authn.Policy: %v", pb)
			}

			if !reflect.DeepEqual(gotProto, c.wantProto) {
				tt.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", gotProto, c.wantProto)
			}
		})
	}
}
