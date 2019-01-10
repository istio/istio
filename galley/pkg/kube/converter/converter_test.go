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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	authn "istio.io/api/authentication/v1alpha1"
	meshcfg "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
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

	if !entries[0].CreationTime.Equal(fakeCreateTime) {
		t.Fatalf("createTime mismatch: got %q want %q",
			entries[0].CreationTime, fakeCreateTime)
	}

	actual, ok := entries[0].Resource.(*types.Struct)
	if !ok {
		t.Fatalf("Unable to convert to struct: %v", entries[0].Resource)
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
					&authn.PeerAuthenticationMethod_Mtls{Mtls: &authn.MutualTls{}},
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
					&authn.PeerAuthenticationMethod_Mtls{Mtls: &authn.MutualTls{}},
				}},
			},
		},
		{
			name:      "nil resource",
			in:        nil,
			wantProto: nil,
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

			gotKey := entries[0].Key
			createTime := entries[0].CreationTime
			pb := entries[0].Resource

			if entries[0].Key != wantKey {
				tt.Fatalf("Keys mismatch. got=%s, want=%s", gotKey, wantKey)
			}

			if c.in == nil {
				return
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

	cases := []struct {
		name      string
		in        *unstructured.Unstructured
		wantProto *extensions.IngressSpec
		cfg       *Config
	}{
		{
			name: "no-conversion",
			in: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "Ingress",
					"metadata": map[string]interface{}{
						"creationTimestamp": fakeCreateTime.Format(time.RFC3339),
						"name":              "foo",
						"namespace":         "default",
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

			wantProto: nil,
		},
		{
			name: "strict",
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

			wantProto: &extensions.IngressSpec{
				Backend: &extensions.IngressBackend{
					ServiceName: "testsvc",
					ServicePort: intstr.IntOrString{Type: intstr.String, StrVal: "80"},
				},
			},
		},
		{
			name: "nil",
			in:   nil,
			cfg: &Config{
				Mesh: meshCfgDefault,
			},

			wantProto: &extensions.IngressSpec{},
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

			if c.wantProto == nil {

				if len(entries) != 0 {
					tt.Fatalf("Expected zero entries: %v", entries)
				}
				return
			}

			if len(entries) != 1 {
				tt.Fatalf("Expected one entry: %v", entries)
			}

			gotKey := entries[0].Key
			createTime := entries[0].CreationTime
			pb := entries[0].Resource

			if entries[0].Key != wantKey {
				tt.Fatalf("Keys mismatch. got=%s, want=%s", gotKey, wantKey)
			}

			if c.in == nil {
				return
			}

			if !createTime.Equal(fakeCreateTime) {
				tt.Fatalf("createTime mismatch: got %q want %q",
					createTime, fakeCreateTime)
			}

			gotProto, ok := pb.(*extensions.IngressSpec)
			if !ok {
				tt.Fatalf("Unable to convert to ingress spec: %v", pb)
			}

			if !reflect.DeepEqual(gotProto, c.wantProto) {
				tt.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", gotProto, c.wantProto)
			}
		})
	}
}

func TestShouldProcessIngress(t *testing.T) {
	istio := model.DefaultMeshConfig().IngressClass
	cases := []struct {
		ingressMode   meshcfg.MeshConfig_IngressControllerMode
		ingressClass  string
		shouldProcess bool
	}{
		{ingressMode: meshcfg.MeshConfig_DEFAULT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: meshcfg.MeshConfig_STRICT, ingressClass: "nginx", shouldProcess: false},
		{ingressMode: meshcfg.MeshConfig_OFF, ingressClass: istio, shouldProcess: false},
		{ingressMode: meshcfg.MeshConfig_DEFAULT, ingressClass: istio, shouldProcess: true},
		{ingressMode: meshcfg.MeshConfig_STRICT, ingressClass: istio, shouldProcess: true},
		{ingressMode: meshcfg.MeshConfig_DEFAULT, ingressClass: "", shouldProcess: true},
		{ingressMode: meshcfg.MeshConfig_STRICT, ingressClass: "", shouldProcess: false},
		{ingressMode: -1, shouldProcess: false},
	}

	for _, c := range cases {
		ing := v1beta1.Ingress{
			ObjectMeta: meta_v1.ObjectMeta{
				Name:        "test-ingress",
				Namespace:   "default",
				Annotations: make(map[string]string),
			},
			Spec: v1beta1.IngressSpec{
				Backend: &v1beta1.IngressBackend{
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
			t.Errorf("shouldProcessIngress(<ingress of class '%s'>) => %v, want %v",
				c.ingressClass, !c.shouldProcess, c.shouldProcess)
		}
	}
}

func TestKubeServiceResource(t *testing.T) {
	cases := []struct {
		name string
		from corev1.Service
		want proto.Message
	}{
		{
			name: "Simple",
			from: corev1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:              "reviews",
					Namespace:         "default",
					CreationTimestamp: meta_v1.Time{Time: fakeCreateTime},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.39.241.161",
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   "TCP",
							Port:       9080,
							TargetPort: intstr.FromInt(9080),
						},
						{
							Name:       "https-web",
							Protocol:   "TCP",
							Port:       9081,
							TargetPort: intstr.FromInt(9081),
						},
						{
							Name:       "ssh",
							Protocol:   "TCP",
							Port:       9082,
							TargetPort: intstr.FromInt(9082),
						},
					},
				},
			},
			want: &networking.ServiceEntry{
				Hosts:      []string{"reviews.default.svc.cluster.local"},
				Addresses:  []string{"10.39.241.161"},
				Resolution: networking.ServiceEntry_STATIC,
				Location:   networking.ServiceEntry_MESH_INTERNAL,
				Ports: []*networking.Port{
					{
						Name:     "http",
						Number:   9080,
						Protocol: "HTTP",
					},
					{
						Name:     "https-web",
						Number:   9081,
						Protocol: "HTTPS",
					},
					{
						Name:     "ssh",
						Number:   9082,
						Protocol: "TCP",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var u unstructured.Unstructured
			u.Object = make(map[string]interface{})
			if err := convertJSON(&c.from, &u.Object); err != nil {
				t.Fatalf("Internal test error: %v", err)
			}
			want := []Entry{{
				Key:          resource.FullNameFromNamespaceAndName(c.from.Namespace, c.from.Name),
				CreationTime: fakeCreateTime.Local(),
				Resource:     c.want,
			}}
			got, err := kubeServiceResource(&Config{DomainSuffix: "cluster.local"}, resource.Info{}, want[0].Key, "kind", &u)
			if err != nil {
				t.Fatalf("kubeServiceResource: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Mismatch:\nGot:\n%v\nWanted:\n%v\n", got, want)
			}
		})
	}
}
