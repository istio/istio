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

package source

import (
	errs "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic/fake"
	dtesting "k8s.io/client-go/testing"

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/converter"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/testing/mock"
)

var emptyInfo resource.Info

func init() {
	b := resource.NewSchemaBuilder()
	b.Register("type.googleapis.com/google.protobuf.Empty")
	schema := b.Build()
	emptyInfo, _ = schema.Lookup("type.googleapis.com/google.protobuf.Empty")
}

func TestNewSource(t *testing.T) {
	k := &mock.Kube{}
	for i := 0; i < 100; i++ {
		cl := fake.NewSimpleDynamicClient(runtime.NewScheme())
		k.AddResponse(cl, nil)
	}

	p, err := New(k, 0)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if p == nil {
		t.Fatal("expected non-nil source")
	}
}

func TestNewSource_Error(t *testing.T) {
	k := &mock.Kube{}
	k.AddResponse(nil, errs.New("newDynamicClient error"))

	_, err := New(k, 0)
	if err == nil || err.Error() != "newDynamicClient error" {
		t.Fatalf("Expected error not found: %v", err)
	}
}

func TestSource_BasicEvents(t *testing.T) {

	k := &mock.Kube{}
	cl := fake.NewSimpleDynamicClient(runtime.NewScheme())

	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "List",
			"metadata": map[string]interface{}{
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{},
		},
	}

	cl.PrependReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	cl.PrependWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	entries := []kube.ResourceSpec{
		{
			Kind:      "List",
			Singular:  "List",
			Plural:    "foos",
			Target:    emptyInfo,
			Converter: converter.Get("identity"),
		},
	}

	s, err := newSource(k, 0, entries)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	log := logChannelOutput(ch, 2)
	expected := strings.TrimSpace(`
[Event](Added: [VKey](type.googleapis.com/google.protobuf.Empty:ns/f1 @rv1))
[Event](FullSync: [VKey](: @))
`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func TestSource_ProtoConversionError(t *testing.T) {
	k := &mock.Kube{}
	cl := fake.NewSimpleDynamicClient(runtime.NewScheme())
	k.AddResponse(cl, nil)

	i1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":            "f1",
				"namespace":       "ns",
				"resourceVersion": "rv1",
			},
			"spec": map[string]interface{}{
				"zoo": "zar",
			},
		},
	}

	cl.PrependReactor("*", "foos", func(action dtesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{i1}}, nil
	})
	cl.PrependWatchReactor("foos", func(action dtesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, mock.NewWatch(), nil
	})

	entries := []kube.ResourceSpec{
		{
			Kind:     "foo",
			Singular: "foo",
			Plural:   "foos",
			Target:   emptyInfo,
			Converter: func(info resource.Info, name string, u *unstructured.Unstructured) (string, time.Time, proto.Message, error) {
				return "", time.Time{}, nil, fmt.Errorf("cant convert")
			},
		},
	}

	s, err := newSource(k, 0, entries)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if s == nil {
		t.Fatal("Expected non nil source")
	}

	ch, err := s.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// The add event should not appear.
	log := logChannelOutput(ch, 1)
	expected := strings.TrimSpace(`
[Event](FullSync: [VKey](: @))
`)
	if log != expected {
		t.Fatalf("Event mismatch:\nActual:\n%s\nExpected:\n%s\n", log, expected)
	}

	s.Stop()
}

func logChannelOutput(ch chan resource.Event, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		item := <-ch
		result += fmt.Sprintf("%v\n", item)
	}

	result = strings.TrimSpace(result)

	return result
}

func TestVerifyCRDPresence(t *testing.T) {
	prevInterval, prevTimeout := crdPresencePollInterval, crdPresensePollTimeout
	crdPresencePollInterval = time.Nanosecond
	crdPresensePollTimeout = time.Millisecond
	defer func() {
		crdPresencePollInterval, crdPresensePollTimeout = prevInterval, prevTimeout
	}()

	specs := []kube.ResourceSpec{
		{
			Plural: "foos",
			Kind:   "Foo",
			Group:  "one.example.com.",
		},
		{
			Plural: "bars",
			Kind:   "Bar",
			Group:  "two.example.com.",
		},
		{
			Plural: "bazs",
			Kind:   "Baz",
			Group:  "three.example.com.",
		},
	}

	type crdDesc struct {
		spec      kube.ResourceSpec
		condition ext.CustomResourceDefinitionCondition
	}

	condition := func(
		typ ext.CustomResourceDefinitionConditionType,
		status ext.ConditionStatus) ext.CustomResourceDefinitionCondition {
		return ext.CustomResourceDefinitionCondition{Type: typ, Status: status}
	}

	cases := []struct {
		name    string
		descs   []crdDesc
		wantErr bool
	}{
		{
			name: "all ready",
			descs: []crdDesc{
				{spec: specs[0], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[1], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[2], condition: condition(ext.Established, ext.ConditionTrue)},
			},
			wantErr: false,
		},
		{
			name: "first not ready",
			descs: []crdDesc{
				{spec: specs[0], condition: condition(ext.Established, ext.ConditionFalse)},
				{spec: specs[1], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[2], condition: condition(ext.Established, ext.ConditionTrue)},
			},
			wantErr: true,
		},
		{
			name: "second not ready",
			descs: []crdDesc{
				{spec: specs[0], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[1], condition: condition(ext.Established, ext.ConditionFalse)},
				{spec: specs[2], condition: condition(ext.Established, ext.ConditionTrue)},
			},
			wantErr: true,
		},
		{
			name: "third not ready",
			descs: []crdDesc{
				{spec: specs[0], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[1], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[2], condition: condition(ext.Established, ext.ConditionFalse)},
			},
			wantErr: true,
		},
		{
			name: "none ready",
			descs: []crdDesc{
				{spec: specs[0], condition: condition(ext.Established, ext.ConditionFalse)},
				{spec: specs[1], condition: condition(ext.Established, ext.ConditionFalse)},
				{spec: specs[2], condition: condition(ext.Established, ext.ConditionFalse)},
			},
			wantErr: true,
		},
		{
			name: "name conflict",
			descs: []crdDesc{
				{spec: specs[0], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[1], condition: condition(ext.Established, ext.ConditionTrue)},
				{spec: specs[2], condition: condition(ext.NamesAccepted, ext.ConditionFalse)},
			},
			wantErr: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			var crds []runtime.Object
			for _, desc := range c.descs {
				crd := &ext.CustomResourceDefinition{
					ObjectMeta: meta_v1.ObjectMeta{
						Name: fmt.Sprintf("%s.%s", desc.spec.Plural, desc.spec.Group),
					},
					Spec: ext.CustomResourceDefinitionSpec{
						Group:   desc.spec.Group,
						Version: "v1",
						Scope:   ext.NamespaceScoped,
						Names: ext.CustomResourceDefinitionNames{
							Plural: desc.spec.Plural,
							Kind:   desc.spec.Kind,
						},
					},
					Status: ext.CustomResourceDefinitionStatus{
						Conditions: []ext.CustomResourceDefinitionCondition{desc.condition},
					},
				}
				crds = append(crds, crd)
			}

			cs := extfake.NewSimpleClientset(crds...)
			err := verifyCRDPresence(cs, specs)
			gotErr := err != nil
			if gotErr != c.wantErr {
				tt.Fatalf("got %v want %v", err, c.wantErr)
			}
		})
	}
}
