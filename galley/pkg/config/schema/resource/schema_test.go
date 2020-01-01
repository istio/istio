// Copyright 2019 Istio Authors
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

package resource

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"
)

func TestCanonicalName(t *testing.T) {
	cases := []struct {
		name     string
		s        Schema
		expected string
	}{
		{
			name: "group",
			s: Builder{
				Group:   "g",
				Version: "v",
				Kind:    "k",
			}.Build(),
			expected: "g/v/k",
		},
		{
			name: "no group",
			s: Builder{
				Group:   "",
				Version: "v",
				Kind:    "k",
			}.Build(),
			expected: "core/v/k",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			g.Expect(c.s.CanonicalResourceName()).To(Equal(c.expected))
		})
	}
}

func TestSchema_NewProtoInstance(t *testing.T) {
	g := NewGomegaWithT(t)

	s := Builder{
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "google.protobuf.Empty",
	}.Build()

	p := s.NewProtoInstance()
	g.Expect(p).To(Equal(&types.Empty{}))
}

func TestSchema_NewProtoInstance_Panic_Nil(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()
	old := protoMessageType
	defer func() {
		protoMessageType = old
	}()
	protoMessageType = func(name string) reflect.Type {
		return nil
	}

	s := Builder{
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "google.protobuf.Empty",
	}.Build()

	_ = s.NewProtoInstance()
}

func TestSchema_NewProtoInstance_Panic_NonProto(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()
	old := protoMessageType
	defer func() {
		protoMessageType = old
	}()
	protoMessageType = func(name string) reflect.Type {
		return reflect.TypeOf(&struct{}{})
	}

	s := Builder{
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "google.protobuf.Empty",
	}.Build()

	_ = s.NewProtoInstance()
}

func TestSchema_Validate(t *testing.T) {
	g := NewGomegaWithT(t)

	s := Builder{
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "google.protobuf.Empty",
	}.Build()

	err := s.Validate()
	g.Expect(err).To(BeNil())
}

func TestSchema_Validate_Failure(t *testing.T) {
	g := NewGomegaWithT(t)

	s := Builder{
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "boo",
	}.Build()

	err := s.Validate()
	g.Expect(err).NotTo(BeNil())
}

func TestSchema_String(t *testing.T) {
	g := NewGomegaWithT(t)

	s := Builder{
		Kind:         "Empty",
		ProtoPackage: "github.com/gogo/protobuf/types",
		Proto:        "google.protobuf.Empty",
	}.Build()

	g.Expect(s.String()).To(Equal(`[Schema](Empty, "github.com/gogo/protobuf/types", google.protobuf.Empty)`))
}
