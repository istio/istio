// Copyright 2018 Istio Authors
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
	extensions "k8s.io/api/extensions/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	authn "istio.io/api/authentication/v1alpha1"
	meshcfg "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"
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

func TestNilConverter(t *testing.T) {
	e, err := nilConverter(nil, resource.Info{}, resource.FullName{}, "", nil)

	if e != nil {
		t.Fatalf("Unexpected entries: %v", e)
	}

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestIdentity(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	info := s.Get("foo")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
				"annotations": map[string]interface{}{
					"a1_key": "a1_value",
					"a2_key": "a2_value",
				},
				"labels": map[string]interface{}{
					"l1_key": "l1_value",
					"l2_key": "l2_value",
				},
			},
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := resource.FullNameFromNamespaceAndName("", "Key")

	entries, err := identity(nil, info, key, "", u)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected one entry: %v", entries)
	}

	if key != entries[0].Key {
		t.Fatalf("Keys mismatch. Wanted=%s, Got=%s", key, entries[0].Key)
	}

	if !entries[0].Metadata.CreateTime.Equal(fakeCreateTime) {
		t.Fatalf("createTime mismatch: got %q want %q",
			entries[0].Metadata.CreateTime, fakeCreateTime)
	}

	actual := entries[0]

	expected := Entry{
		Key: key,
		Metadata: resource.Metadata{
			Annotations: map[string]string{
				"a1_key": "a1_value",
				"a2_key": "a2_value",
			},
			Labels: map[string]string{
				"l1_key": "l1_value",
				"l2_key": "l2_value",
			},
			CreateTime: fakeCreateTime.Local(),
		},
		Resource: &types.Struct{
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

func TestIdentity_Error(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Empty")
	s := b.Build()

	info := s.Get("foo")

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind": "k1",
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	key := resource.FullNameFromNamespaceAndName("", "Key")

	_, err := identity(nil, info, key, "", u)
	if err == nil {
		t.Fatal("Expected error not found")
	}
}

func TestIdentity_NilResource(t *testing.T) {
	b := resource.NewSchemaBuilder()
	b.Register("foo", "type.googleapis.com/google.protobuf.Struct")
	s := b.Build()

	info := s.Get("foo")

	key := resource.FullNameFromNamespaceAndName("foo", "Key")

	entries, err := identity(nil, info, key, "knd", nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected one entry: %v", entries)
	}

	if key != entries[0].Key {
		t.Fatalf("Keys mismatch. Wanted=%s, Got=%s", key, entries[0].Key)
	}
}

func TestAuthPolicyResource(t *testing.T) {
	typeURL := fmt.Sprintf("type.googleapis.com/" + proto.MessageName((*authn.Policy)(nil)))
	collection := "test/collection/authpolicy"

	b := resource.NewSchemaBuilder()
	b.Register(collection, typeURL)
	s := b.Build()

	info := s.Get(collection)

	cases := []struct {
		name string
		in   *unstructured.Unstructured
		want Entry
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
						"annotations": map[string]interface{}{
							"a1_key": "a1_value",
							"a2_key": "a2_value",
						},
						"labels": map[string]interface{}{
							"l1_key": "l1_value",
							"l2_key": "l2_value",
						},
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
			want: Entry{
				Key: resource.FullNameFromNamespaceAndName("default", "foo"),
				Metadata: resource.Metadata{
					Annotations: map[string]string{
						"a1_key": "a1_value",
						"a2_key": "a2_value",
					},
					Labels: map[string]string{
						"l1_key": "l1_value",
						"l2_key": "l2_value",
					},
					CreateTime: fakeCreateTime.Local(),
				},
				Resource: &authn.Policy{
					Targets: []*authn.TargetSelector{{
						Name: "foo",
					}},
					Peers: []*authn.PeerAuthenticationMethod{{
						Params: &authn.PeerAuthenticationMethod_Mtls{Mtls: &authn.MutualTls{}},
					}},
				},
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
						"annotations": map[string]interface{}{
							"a1_key": "a1_value",
							"a2_key": "a2_value",
						},
						"labels": map[string]interface{}{
							"l1_key": "l1_value",
							"l2_key": "l2_value",
						},
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
			want: Entry{
				Key: resource.FullNameFromNamespaceAndName("default", "foo"),
				Metadata: resource.Metadata{
					Annotations: map[string]string{
						"a1_key": "a1_value",
						"a2_key": "a2_value",
					},
					Labels: map[string]string{
						"l1_key": "l1_value",
						"l2_key": "l2_value",
					},
					CreateTime: fakeCreateTime.Local(),
				},
				Resource: &authn.Policy{
					Targets: []*authn.TargetSelector{{
						Name: "foo",
					}},
					Peers: []*authn.PeerAuthenticationMethod{{
						Params: &authn.PeerAuthenticationMethod_Mtls{Mtls: &authn.MutualTls{}},
					}},
				},
			},
		},
		{
			name: "nil resource",
			in:   nil,
			want: Entry{
				Key:      resource.FullNameFromNamespaceAndName("ns1", "res1"),
				Resource: nil,
			},
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(tt *testing.T) {
			wantKey := resource.FullNameFromNamespaceAndName("ns1", "res1")
			if c.in != nil {
				wantKey = resource.FullNameFromNamespaceAndName(c.in.GetNamespace(), c.in.GetName())
			}
			entries, err := authPolicyResource(nil, info, wantKey, "kind", c.in)
			if err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}

			if len(entries) != 1 {
				tt.Fatalf("Expected one entry: %v", entries)
			}

			got := entries[0]

			if !reflect.DeepEqual(got, c.want) {
				tt.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", got, c.want)
			}
		})
	}
}

func TestKubeIngressResource(t *testing.T) {
	typeURL := fmt.Sprintf("type.googleapis.com/" + proto.MessageName((*extensions.IngressSpec)(nil)))
	collection := "test/collection/ingress"

	b := resource.NewSchemaBuilder()
	b.Register(collection, typeURL)
	s := b.Build()

	info := s.Get(collection)

	meshCfgOff := meshconfig.NewInMemory()
	meshCfgStrict := meshconfig.NewInMemory()
	meshCfgStrict.Set(meshcfg.MeshConfig{
		IngressClass:          "cls",
		IngressControllerMode: meshcfg.MeshConfig_STRICT,
	})
	meshCfgDefault := meshconfig.NewInMemory()
	meshCfgDefault.Set(meshcfg.MeshConfig{
		IngressClass:          "cls",
		IngressControllerMode: meshcfg.MeshConfig_DEFAULT,
	})

	var nilIngress *extensions.IngressSpec
	cases := []struct {
		name          string
		shouldConvert bool
		in            *unstructured.Unstructured
		want          Entry
		cfg           *Config
	}{
		{
			name:          "no-conversion",
			shouldConvert: false,
			in: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Ingress",
					"metadata": map[string]interface{}{
						"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
						"name":              "foo",
						"namespace":         "default",
						"annotations": map[string]interface{}{
							"a1_key": "a1_value",
							"a2_key": "a2_value",
						},
						"labels": map[string]interface{}{
							"l1_key": "l1_value",
							"l2_key": "l2_value",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "testsvc",
							"servicePort": "80",
						},
					},
				},
			},
			cfg: &Config{
				Mesh: meshCfgOff,
			},

			want: Entry{
				Key: resource.FullNameFromNamespaceAndName("default", "foo"),
				Metadata: resource.Metadata{
					Annotations: map[string]string{
						"a1_key": "a1_value",
						"a2_key": "a2_value",
					},
					Labels: map[string]string{
						"l1_key": "l1_value",
						"l2_key": "l2_value",
					},
					CreateTime: fakeCreateTime.Local(),
				},
				Resource: nil,
			},
		},
		{
			name:          "strict",
			shouldConvert: true,
			in: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Ingress",
					"metadata": map[string]interface{}{
						"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
						"name":              "foo",
						"namespace":         "default",
						"annotations": map[string]interface{}{
							"kubernetes.io/ingress.class": "cls",
						},
						"labels": map[string]interface{}{
							"l1_key": "l1_value",
							"l2_key": "l2_value",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "testsvc",
							"servicePort": "80",
						},
					},
				},
			},
			cfg: &Config{
				Mesh: meshCfgStrict,
			},

			want: Entry{
				Key: resource.FullNameFromNamespaceAndName("default", "foo"),
				Metadata: resource.Metadata{
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "cls",
					},
					Labels: map[string]string{
						"l1_key": "l1_value",
						"l2_key": "l2_value",
					},
					CreateTime: fakeCreateTime.Local(),
				},
				Resource: &extensions.IngressSpec{
					Backend: &extensions.IngressBackend{
						ServiceName: "testsvc",
						ServicePort: intstr.IntOrString{Type: intstr.String, StrVal: "80"},
					},
				},
			},
		},
		{
			name:          "nil",
			shouldConvert: true,
			in:            nil,
			cfg: &Config{
				Mesh: meshCfgDefault,
			},

			want: Entry{
				Key:      resource.FullNameFromNamespaceAndName("ns1", "res1"),
				Resource: nilIngress,
			},
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%d] %s", i, c.name), func(tt *testing.T) {
			wantKey := resource.FullNameFromNamespaceAndName("ns1", "res1")
			if c.in != nil {
				wantKey = resource.FullNameFromNamespaceAndName(c.in.GetNamespace(), c.in.GetName())
			}
			entries, err := kubeIngressResource(c.cfg, info, wantKey, "kind", c.in)
			if err != nil {
				tt.Fatalf("Unexpected error: %v", err)
			}

			if !c.shouldConvert {
				if len(entries) != 0 {
					tt.Fatalf("Expected zero entries: %v", entries)
				}
				return
			}

			if len(entries) != 1 {
				tt.Fatalf("Expected one entry: %v", entries)
			}

			got := entries[0]

			if !reflect.DeepEqual(got.Resource, c.want.Resource) {
				tt.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", got.Resource, c.want.Resource)
			}
		})
	}
}

func TestShouldProcessIngress(t *testing.T) {
	istio := model.DefaultMeshConfig().IngressClass
	cases := []struct {
		ingressClass  string
		ingressMode   meshcfg.MeshConfig_IngressControllerMode
		shouldProcess bool
	}{
		{ingressMode: meshcfg.MeshConfig_DEFAULT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: meshcfg.MeshConfig_STRICT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: meshcfg.MeshConfig_OFF, ingressClass: istio, shouldProcess: false},
		{ingressMode: meshcfg.MeshConfig_DEFAULT, ingressClass: istio, shouldProcess: true},
		{ingressMode: meshcfg.MeshConfig_STRICT, ingressClass: istio, shouldProcess: false},
		{ingressMode: meshcfg.MeshConfig_DEFAULT, ingressClass: "", shouldProcess: true},
		{ingressMode: meshcfg.MeshConfig_STRICT, ingressClass: "", shouldProcess: false},
		{ingressMode: -1, shouldProcess: false},
	}

	for _, c := range cases {
		ing := extensions.Ingress{
			ObjectMeta: metaV1.ObjectMeta{
				Name:        "test-ingress",
				Namespace:   "default",
				Annotations: make(map[string]string),
			},
			Spec: extensions.IngressSpec{
				Backend: &extensions.IngressBackend{
					ServiceName: "default-http-backend",
					ServicePort: intstr.FromInt(80),
				},
			},
		}

		mesh := model.DefaultMeshConfig()
		mesh.IngressControllerMode = c.ingressMode
		cch := meshconfig.NewInMemory()
		cch.Set(mesh)
		cfg := Config{Mesh: cch}

		if c.ingressClass != "" {
			ing.Annotations["kubernetes.io/ingress.class"] = c.ingressClass
		}

		if c.shouldProcess != shouldProcessIngress(&cfg, &ing) {
			t.Errorf("shouldProcessIngress(<ingress of class '%s'>) => %v, want %v for mesh config %v",
				c.ingressClass, !c.shouldProcess, c.shouldProcess, c.ingressMode)
		}
	}
}
