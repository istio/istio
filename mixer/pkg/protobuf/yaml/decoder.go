// Copyright Istio Authors.
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

package yaml

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/protobuf/yaml/wire"
	"istio.io/pkg/attribute"
)

type (
	// Decoder transforms protobuf-encoded bytes to attribute values.
	Decoder struct {
		resolver Resolver
		fields   map[wire.Number]*descriptor.FieldDescriptorProto
	}
)

// NewDecoder creates a decoder specific to a dynamic proto descriptor.
// Additionally, it takes as input an optional field mask to avoid decoding
// unused field values. Field mask is keyed by the message proto field names.
// A nil field mask implies all fields are decoded.
// This decoder is specialized to a single-level proto schema (no nested field dereferences
// in the resulting output).
func NewDecoder(resolver Resolver, msgName string, fieldMask map[string]bool) *Decoder {
	message := resolver.ResolveMessage(msgName)
	fields := make(map[wire.Number]*descriptor.FieldDescriptorProto)

	for _, f := range message.Field {
		if fieldMask == nil || fieldMask[f.GetName()] {
			fields[f.GetNumber()] = f
		}
	}

	return &Decoder{
		resolver: resolver,
		fields:   fields,
	}
}

// Decode function parses wire-encoded bytes to attribute values. The keys are field names
// in the message specified by the field mask.
// Attribute prefix is appended to the field names in the output attribute bag.
func (d *Decoder) Decode(b []byte, out *attribute.MutableBag, attrPrefix string) error {
	visitor := &decodeVisitor{
		decoder:    d,
		out:        out,
		attrPrefix: attrPrefix,
	}

	for len(b) > 0 {
		_, _, n := wire.ConsumeField(visitor, b)
		if n < 0 {
			return wire.ParseError(n)
		}
		b = b[n:]
	}

	return visitor.err
}

type decodeVisitor struct {
	decoder    *Decoder
	out        *attribute.MutableBag
	attrPrefix string
	err        error
}

func (dv *decodeVisitor) setValue(f *descriptor.FieldDescriptorProto, val interface{}) {
	name := dv.attrPrefix + f.GetName()
	if f.IsRepeated() {
		var arr *attribute.List
		old, ok := dv.out.Get(name)
		if !ok {
			arr = attribute.NewList(name)
			dv.out.Set(name, arr)
		} else {
			arr = old.(*attribute.List)
		}
		arr.Append(val)
	} else {
		dv.out.Set(name, val)
	}
}

// varint coalesces all primitive integers and enums to int64 type
func (dv *decodeVisitor) Varint(n wire.Number, v uint64) {
	f, exists := dv.decoder.fields[n]
	if !exists {
		return
	}
	var val interface{}
	switch f.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		val = wire.DecodeBool(v)
	case descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_UINT64:
		val = int64(v)
	case descriptor.FieldDescriptorProto_TYPE_SINT32,
		descriptor.FieldDescriptorProto_TYPE_SINT64:
		val = wire.DecodeZigZag(v)
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		val = int64(v)
	default:
		dv.err = multierror.Append(dv.err, fmt.Errorf("unexpected field type %q for varint encoding", f.GetType()))
		return
	}
	dv.setValue(f, val)
}

func (dv *decodeVisitor) Fixed32(n wire.Number, v uint32) {
	f, exists := dv.decoder.fields[n]
	if !exists {
		return
	}
	var val interface{}
	switch f.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_FIXED32:
		val = int64(v)
	case descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		val = int64(v)
	case descriptor.FieldDescriptorProto_TYPE_FLOAT:
		val = float64(math.Float32frombits(v))
	default:
		dv.err = multierror.Append(dv.err, fmt.Errorf("unexpected field type %q for fixed32 encoding", f.GetType()))
		return
	}
	dv.setValue(f, val)
}

func (dv *decodeVisitor) Fixed64(n wire.Number, v uint64) {
	f, exists := dv.decoder.fields[n]
	if !exists {
		return
	}
	var val interface{}
	switch f.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_FIXED64:
		val = int64(v)
	case descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		val = int64(v)
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		val = math.Float64frombits(v)
	default:
		dv.err = multierror.Append(dv.err, fmt.Errorf("unexpected field type %q for fixed64 encoding", f.GetType()))
		return
	}
	dv.setValue(f, val)
}

type mapVisitor struct {
	desc       *descriptor.DescriptorProto
	key, value string
}

// TODO: only string maps are supported since mixer's IL only supports string maps
func (mv *mapVisitor) Varint(wire.Number, uint64)  {}
func (mv *mapVisitor) Fixed32(wire.Number, uint32) {}
func (mv *mapVisitor) Fixed64(wire.Number, uint64) {}
func (mv *mapVisitor) Bytes(n wire.Number, v []byte) {
	switch n {
	case mv.desc.Field[0].GetNumber():
		mv.key = string(v)
	case mv.desc.Field[1].GetNumber():
		mv.value = string(v)
	}
}

func (dv *decodeVisitor) Bytes(n wire.Number, v []byte) {
	f, exists := dv.decoder.fields[n]
	if !exists {
		return
	}
	switch f.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		dv.setValue(f, string(v))
		return
	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		dv.setValue(f, v)
		return
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		name := dv.attrPrefix + f.GetName()
		if isMap(dv.decoder.resolver, f) {
			// validate proto type to be map<string, string>
			mapType := dv.decoder.resolver.ResolveMessage(f.GetTypeName())

			if mapType == nil || len(mapType.Field) < 2 {
				dv.err = multierror.Append(dv.err, fmt.Errorf("unresolved or incorrect map field type %q", f.GetName()))
				return
			}

			if mapType.Field[0].GetType() != descriptor.FieldDescriptorProto_TYPE_STRING ||
				mapType.Field[1].GetType() != descriptor.FieldDescriptorProto_TYPE_STRING {
				dv.err = multierror.Append(dv.err, errors.New("only map<string, string> is supported in expressions"))
				return
			}

			// translate map<X, Y> proto3 field type to record Mixer type (map[string]string)
			var m attribute.StringMap
			val, ok := dv.out.Get(name)
			if !ok {
				m = attribute.NewStringMap(name, make(map[string]string, 1), nil)
			} else {
				m = val.(attribute.StringMap)
			}

			visitor := &mapVisitor{desc: mapType}
			for len(v) > 0 {
				_, _, m := wire.ConsumeField(visitor, v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse map field %q: %v", f.GetName(), wire.ParseError(m)))
					return
				}
				v = v[m:]
			}

			m.Set(visitor.key, visitor.value)
			dv.out.Set(name, m)
			return
		}

		// parse the entirety of the message recursively for value types
		if _, ok := valueResolver.descriptors[f.GetTypeName()]; ok && !f.IsRepeated() {
			decoder := NewDecoder(valueResolver, f.GetTypeName(), nil)
			inner := attribute.GetMutableBag(nil)
			defer inner.Done()
			if err := decoder.Decode(v, inner, ""); err != nil {
				dv.err = multierror.Append(dv.err, fmt.Errorf("failed to decode field %q: %v", f.GetName(), err))
				return
			}

			switch f.GetTypeName() {
			case ".istio.policy.v1beta1.Value":
				// pop oneof inner value from the bag to the field value
				names := inner.Names()
				if len(names) == 1 {
					if val, ok := inner.Get(names[0]); ok {
						dv.out.Set(name, val)
					}
				}
			case ".istio.policy.v1beta1.IPAddress",
				".istio.policy.v1beta1.Duration",
				".istio.policy.v1beta1.DNSName":
				if value, ok := inner.Get("value"); ok {
					dv.out.Set(name, value)
				}
			case ".google.protobuf.Duration":
				seconds, secondsOk := inner.Get("seconds")
				nanos, nanosOk := inner.Get("nanos")
				if secondsOk || nanosOk {
					// must default to int64 for casting to work
					if seconds == nil {
						seconds = int64(0)
					}
					if nanos == nil {
						nanos = int64(0)
					}
					dv.out.Set(name, time.Duration(seconds.(int64))*time.Second+time.Duration(nanos.(int64)))
				}
			}

			return
		}

		// TODO: implement sub-message decoding
		return
	}

	// fallback into packed repeated encoding
	if f.IsRepeated() && (f.IsPacked() || f.IsPacked3()) {
		var m int
		for len(v) > 0 {
			switch f.GetType() {
			case descriptor.FieldDescriptorProto_TYPE_BOOL,
				descriptor.FieldDescriptorProto_TYPE_INT32,
				descriptor.FieldDescriptorProto_TYPE_INT64,
				descriptor.FieldDescriptorProto_TYPE_UINT32,
				descriptor.FieldDescriptorProto_TYPE_UINT64,
				descriptor.FieldDescriptorProto_TYPE_SINT32,
				descriptor.FieldDescriptorProto_TYPE_SINT64,
				descriptor.FieldDescriptorProto_TYPE_ENUM:
				var elt uint64
				elt, m = wire.ConsumeVarint(v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse packed varint field %q", f.GetName()))
					return
				}
				dv.Varint(n, elt)

			case descriptor.FieldDescriptorProto_TYPE_FIXED32,
				descriptor.FieldDescriptorProto_TYPE_SFIXED32,
				descriptor.FieldDescriptorProto_TYPE_FLOAT:
				var elt uint32
				elt, m = wire.ConsumeFixed32(v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse packed fixed32 field %q", f.GetName()))
					return
				}
				dv.Fixed32(n, elt)

			case descriptor.FieldDescriptorProto_TYPE_FIXED64,
				descriptor.FieldDescriptorProto_TYPE_SFIXED64,
				descriptor.FieldDescriptorProto_TYPE_DOUBLE:
				var elt uint64
				elt, m = wire.ConsumeFixed64(v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse packed fixed64 field %q", f.GetName()))
					return
				}
				dv.Fixed64(n, elt)

			default:
				dv.err = multierror.Append(dv.err, fmt.Errorf("unexpected field type %q for packed repeated bytes encoding", f.GetType()))
				return
			}
			v = v[m:]
		}
		return
	}

	dv.err = multierror.Append(dv.err, fmt.Errorf("unexpected field type %q for bytes encoding", f.GetType()))
}

type valueTypeResolver struct {
	descriptors map[string]*descriptor.DescriptorProto
}

func getDescriptorProto(msg descriptor.Message) *descriptor.DescriptorProto {
	_, desc := descriptor.ForMessage(msg)
	return desc
}

func newValueTypeResolver() *valueTypeResolver {
	out := &valueTypeResolver{
		descriptors: map[string]*descriptor.DescriptorProto{
			".istio.policy.v1beta1.Value":     getDescriptorProto(&v1beta1.Value{}),
			".istio.policy.v1beta1.IPAddress": getDescriptorProto(&v1beta1.IPAddress{}),
			".istio.policy.v1beta1.Duration":  getDescriptorProto(&v1beta1.Duration{}),
			".istio.policy.v1beta1.TimeStamp": getDescriptorProto(&v1beta1.TimeStamp{}),
			".istio.policy.v1beta1.DNSName":   getDescriptorProto(&v1beta1.DNSName{}),
			".google.protobuf.Duration":       getDescriptorProto(&types.Duration{}),
			".google.protobuf.Timestamp":      getDescriptorProto(&types.Timestamp{}),
		},
	}

	return out
}

func (vtr *valueTypeResolver) ResolveMessage(msg string) *descriptor.DescriptorProto {
	return vtr.descriptors[msg]
}

func (vtr *valueTypeResolver) ResolveEnum(string) *descriptor.EnumDescriptorProto {
	return nil
}

func (vtr *valueTypeResolver) ResolveService(string) (*descriptor.ServiceDescriptorProto, string) {
	return nil, ""
}

var (
	valueResolver = newValueTypeResolver()
)

// DecodeType converts protobuf types to mixer's IL types.
// Note that due to absence of corresponding types for a variety of concrete values produced by the decoder,
// the returned value may be an unspecified type.
func DecodeType(resolver Resolver, desc *descriptor.FieldDescriptorProto) v1beta1.ValueType {
	if desc.GetType() == descriptor.FieldDescriptorProto_TYPE_MESSAGE {
		fieldType := resolver.ResolveMessage(desc.GetTypeName())

		if fieldType != nil && len(fieldType.Field) == 2 &&
			fieldType.GetOptions().GetMapEntry() &&
			fieldType.Field[0].GetType() == descriptor.FieldDescriptorProto_TYPE_STRING &&
			fieldType.Field[1].GetType() == descriptor.FieldDescriptorProto_TYPE_STRING {
			return v1beta1.STRING_MAP
		}

		if desc.IsRepeated() {
			return v1beta1.VALUE_TYPE_UNSPECIFIED
		}

		switch desc.GetTypeName() {
		case ".istio.policy.v1beta1.Value":
			return v1beta1.VALUE_TYPE_UNSPECIFIED
		case ".istio.policy.v1beta1.IPAddress":
			return v1beta1.IP_ADDRESS
		case ".istio.policy.v1beta1.Duration", ".google.protobuf.Duration":
			return v1beta1.DURATION
		case ".istio.policy.v1beta1.TimeStamp", ".google.protobuf.Timestamp":
			return v1beta1.TIMESTAMP
		case ".istio.policy.v1beta1.DNSName":
			return v1beta1.DNS_NAME
		}
	}

	// array types are missing
	if desc.IsRepeated() {
		return v1beta1.VALUE_TYPE_UNSPECIFIED
	}

	switch desc.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		return v1beta1.BOOL
	case descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_UINT64,
		descriptor.FieldDescriptorProto_TYPE_SINT32,
		descriptor.FieldDescriptorProto_TYPE_SINT64,
		descriptor.FieldDescriptorProto_TYPE_ENUM,
		descriptor.FieldDescriptorProto_TYPE_FIXED32,
		descriptor.FieldDescriptorProto_TYPE_SFIXED32,
		descriptor.FieldDescriptorProto_TYPE_FIXED64,
		descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		return v1beta1.INT64
	case descriptor.FieldDescriptorProto_TYPE_FLOAT,
		descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		return v1beta1.DOUBLE
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		return v1beta1.STRING
	}
	return v1beta1.VALUE_TYPE_UNSPECIFIED
}
