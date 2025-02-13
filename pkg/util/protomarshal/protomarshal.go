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

// Package protomarshal provides operations to marshal and unmarshal protobuf objects.
// Unlike the rest of this repo, which uses the new google.golang.org/protobuf API, this package
// explicitly uses the legacy jsonpb package. This is due to a number of compatibility concerns with the new API:
// * https://github.com/golang/protobuf/issues/1374
// * https://github.com/golang/protobuf/issues/1373
package protomarshal

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/golang/protobuf/jsonpb"             // nolint: depguard
	legacyproto "github.com/golang/protobuf/proto"  // nolint: staticcheck
	"google.golang.org/protobuf/encoding/protojson" // nolint: depguard
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/structpb"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/log"
)

var (
	unmarshaler       = jsonpb.Unmarshaler{AllowUnknownFields: true}
	strictUnmarshaler = jsonpb.Unmarshaler{}
)

func Unmarshal(b []byte, m proto.Message) error {
	return strictUnmarshaler.Unmarshal(bytes.NewReader(b), legacyproto.MessageV1(m))
}

func UnmarshalString(s string, m proto.Message) error {
	return Unmarshal([]byte(s), m)
}

func UnmarshalAllowUnknown(b []byte, m proto.Message) error {
	return unmarshaler.Unmarshal(bytes.NewReader(b), legacyproto.MessageV1(m))
}

type resolver interface {
	protoregistry.MessageTypeResolver
	protoregistry.ExtensionTypeResolver
}

func UnmarshalAllowUnknownWithAnyResolver(anyResolver resolver, b []byte, m proto.Message) error {
	return (&protojson.UnmarshalOptions{
		DiscardUnknown: true,
		Resolver:       anyResolver,
	}).Unmarshal(b, m)
}

// ToJSON marshals a proto to canonical JSON
func ToJSON(msg proto.Message) (string, error) {
	return ToJSONWithIndent(msg, "")
}

// Marshal marshals a proto to canonical JSON
func Marshal(msg proto.Message) ([]byte, error) {
	res, err := ToJSONWithIndent(msg, "")
	if err != nil {
		return nil, err
	}
	return []byte(res), err
}

// MarshalIndent marshals a proto to canonical JSON with indentation
func MarshalIndent(msg proto.Message, indent string) ([]byte, error) {
	res, err := ToJSONWithIndent(msg, indent)
	if err != nil {
		return nil, err
	}
	return []byte(res), err
}

// MarshalIndentWithGlobalTypesResolver marshals a proto to canonical JSON with indentation
// and multiline while using generic types resolver
func MarshalIndentWithGlobalTypesResolver(msg proto.Message, indent string) ([]byte, error) {
	return protojson.MarshalOptions{
		Multiline: true,
		Indent:    indent,
	}.Marshal(msg)
}

// MarshalProtoNames marshals a proto to canonical JSON original protobuf names
func MarshalProtoNames(msg proto.Message) ([]byte, error) {
	if msg == nil {
		return nil, errors.New("unexpected nil message")
	}

	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{OrigName: true}
	buf := &bytes.Buffer{}
	err := m.Marshal(buf, legacyproto.MessageV1(msg))
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToJSONWithIndent marshals a proto to canonical JSON with pretty printed string
func ToJSONWithIndent(msg proto.Message, indent string) (string, error) {
	return ToJSONWithOptions(msg, indent, false)
}

// ToJSONWithOptions marshals a proto to canonical JSON with options to indent and
// print enums' int values
func ToJSONWithOptions(msg proto.Message, indent string, enumsAsInts bool) (string, error) {
	if msg == nil {
		return "", errors.New("unexpected nil message")
	}

	// Marshal from proto to json bytes
	m := jsonpb.Marshaler{Indent: indent, EnumsAsInts: enumsAsInts}
	return m.MarshalToString(legacyproto.MessageV1(msg))
}

func ToJSONWithAnyResolver(msg proto.Message, indent string, anyResolver jsonpb.AnyResolver) (string, error) {
	if msg == nil {
		return "", errors.New("unexpected nil message")
	}

	m := jsonpb.Marshaler{
		Indent:      indent,
		AnyResolver: anyResolver,
	}
	return m.MarshalToString(legacyproto.MessageV1(msg))
}

// ToYAML marshals a proto to canonical YAML
func ToYAML(msg proto.Message) (string, error) {
	js, err := ToJSON(msg)
	if err != nil {
		return "", err
	}
	yml, err := yaml.JSONToYAML([]byte(js))
	return string(yml), err
}

// ToJSONMap converts a proto message to a generic map using canonical JSON encoding
// JSON encoding is specified here: https://developers.google.com/protocol-buffers/docs/proto3#json
func ToJSONMap(msg proto.Message) (map[string]any, error) {
	js, err := ToJSON(msg)
	if err != nil {
		return nil, err
	}

	// Unmarshal from json bytes to go map
	var data map[string]any
	err = json.Unmarshal([]byte(js), &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ApplyJSON unmarshals a JSON string into a proto message.
func ApplyJSON(js string, pb proto.Message) error {
	reader := strings.NewReader(js)
	m := jsonpb.Unmarshaler{}
	if err := m.Unmarshal(reader, legacyproto.MessageV1(pb)); err != nil {
		log.Debugf("Failed to decode proto: %q. Trying decode with AllowUnknownFields=true", err)
		m.AllowUnknownFields = true
		reader.Reset(js)
		return m.Unmarshal(reader, legacyproto.MessageV1(pb))
	}
	return nil
}

// ApplyJSONStrict unmarshals a JSON string into a proto message.
func ApplyJSONStrict(js string, pb proto.Message) error {
	reader := strings.NewReader(js)
	m := jsonpb.Unmarshaler{}
	return m.Unmarshal(reader, legacyproto.MessageV1(pb))
}

// ApplyYAML unmarshals a YAML string into a proto message.
// Unknown fields are allowed.
func ApplyYAML(yml string, pb proto.Message) error {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return err
	}
	return ApplyJSON(string(js), pb)
}

// ApplyYAMLStrict unmarshals a YAML string into a proto message.
// Unknown fields are not allowed.
func ApplyYAMLStrict(yml string, pb proto.Message) error {
	js, err := yaml.YAMLToJSON([]byte(yml))
	if err != nil {
		return err
	}
	return ApplyJSONStrict(string(js), pb)
}

type ComparableMessage interface {
	comparable
	proto.Message
}

// ShallowClone performs a shallow clone of the object. For a deep clone, use Clone.
func ShallowClone[T ComparableMessage](src T) T {
	var empty T
	if src == empty {
		return empty
	}
	dst := src.ProtoReflect().New().Interface().(T)
	dm := dst.ProtoReflect()
	sm := src.ProtoReflect()
	if dm.Type() != sm.Type() {
		panic("mismatching type")
	}
	proto.Reset(dst)
	sm.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		dm.Set(fd, v)
		return true
	})
	return dst
}

// Clone is a small wrapper that handles the upstream function not returning a typed message
func Clone[T proto.Message](obj T) T {
	return proto.Clone(obj).(T)
}

// MessageToStructSlow encodes a protobuf Message into a Struct.
// It roundtrips trough JSON so it is slow.
// Copied from https://github.com/envoyproxy/go-control-plane/blob/d77bd2ea68bdbb72afd65a7ddf6fe8969e556c45/pkg/conversion/struct.go#L29
func MessageToStructSlow(msg proto.Message) (*structpb.Struct, error) {
	if msg == nil {
		return nil, errors.New("nil message")
	}

	b, err := (&protojson.MarshalOptions{UseProtoNames: true}).Marshal(msg)
	if err != nil {
		return nil, err
	}

	pbs := &structpb.Struct{}
	if err := protojson.Unmarshal(b, pbs); err != nil {
		return nil, err
	}

	return pbs, nil
}

// StructToMessageSlow decodes a protobuf Message from a Struct.
// It roundtrips trough JSON so it is slow.
// Copied from https://github.com/envoyproxy/go-control-plane/blob/d77bd2ea68bdbb72afd65a7ddf6fe8969e556c45/pkg/conversion/struct.go#L48
func StructToMessageSlow(pbst *structpb.Struct, out proto.Message) error {
	if pbst == nil {
		return errors.New("nil struct")
	}

	b, err := (&protojson.MarshalOptions{UseProtoNames: true}).Marshal(pbst)
	if err != nil {
		return err
	}

	return protojson.Unmarshal(b, out)
}
