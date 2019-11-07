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

package collection

import (
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"
	. "github.com/onsi/gomega"
)

func TestSpec_NewSpec(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := NewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	g.Expect(err).To(BeNil())
	g.Expect(s.Name).To(Equal(NewName("foo")))
	g.Expect(s.ProtoPackage).To(Equal("github.com/gogo/protobuf/types"))
	g.Expect(s.MessageName).To(Equal("google.protobuf.Empty"))
}

func TestSpec_NewSpec_Error(t *testing.T) {
	g := NewGomegaWithT(t)

	_, err := NewSpec("$", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	g.Expect(err).NotTo(BeNil())
}

func TestSpec_MustNewSpec(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).To(BeNil())
	}()

	s := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	g.Expect(s.Name).To(Equal(NewName("foo")))
	g.Expect(s.ProtoPackage).To(Equal("github.com/gogo/protobuf/types"))
	g.Expect(s.MessageName).To(Equal("google.protobuf.Empty"))
}

func TestSpec_MustNewSpec_Error(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	MustNewSpec("$", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
}

func TestSpec_NewProtoInstance(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := NewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	g.Expect(err).To(BeNil())

	p := s.NewProtoInstance()
	g.Expect(p).To(Equal(&types.Empty{}))
}

func TestSpec_NewProtoInstance_Panic_Nil(t *testing.T) {
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

	s, err := NewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	g.Expect(err).To(BeNil())

	_ = s.NewProtoInstance()
}

func TestSpec_NewProtoInstance_Panic_NonProto(t *testing.T) {
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

	s, err := NewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	g.Expect(err).To(BeNil())

	_ = s.NewProtoInstance()
}

func TestSpec_Validate(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := NewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	g.Expect(err).To(BeNil())

	err = s.Validate()
	g.Expect(err).To(BeNil())
}

func TestSpec_Validate_Failure(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := NewSpec("foo", "github.com/gogo/protobuf/types", "boo")
	g.Expect(err).To(BeNil())

	err = s.Validate()
	g.Expect(err).NotTo(BeNil())
}

func TestSpec_String(t *testing.T) {
	g := NewGomegaWithT(t)
	b := NewSpecsBuilder()

	spec := MustNewSpec("foo", "github.com/gogo/protobuf/types", "google.protobuf.Empty")
	b.MustAdd(spec)

	g.Expect(spec.String()).To(Equal(`[Spec](foo, "github.com/gogo/protobuf/types", google.protobuf.Empty)`))
}
