// Copyright 2018 Istio Authors.
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
	"io"
	"math"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	multierror "github.com/hashicorp/go-multierror"
)

type (
	// Decoder transforms protobuf-encoded bytes to attribute values.
	Decoder struct {
		resolver Resolver
		fields   map[Number]*descriptor.FieldDescriptorProto
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
	fields := make(map[Number]*descriptor.FieldDescriptorProto)

	for _, f := range message.Field {
		if fieldMask == nil || fieldMask[f.GetJsonName()] || fieldMask[f.GetName()] {
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
func (d *Decoder) Decode(b []byte) (map[string]interface{}, error) {
	visitor := &decodeVisitor{
		decoder: d,
		out:     make(map[string]interface{}),
	}

	for len(b) > 0 {
		_, _, n := ConsumeField(visitor, b)
		if n < 0 {
			return visitor.out, ParseError(n)
		}
		b = b[n:]
	}

	return visitor.out, visitor.err
}

type decodeVisitor struct {
	decoder *Decoder
	out     map[string]interface{}
	err     error
}

func (dv *decodeVisitor) setValue(f *descriptor.FieldDescriptorProto, val interface{}) {
	if f.IsRepeated() {
		var arr []interface{}
		old, ok := dv.out[f.GetName()]
		if !ok {
			arr = make([]interface{}, 0, 1)
		} else {
			arr = old.([]interface{})
		}
		dv.out[f.GetName()] = append(arr, val)
	} else {
		dv.out[f.GetName()] = val
	}
}

// varint coalesces all primitive integers and enums to int64 type
func (dv *decodeVisitor) varint(n Number, v uint64) {
	f, exists := dv.decoder.fields[n]
	if !exists {
		return
	}
	var val interface{}
	switch f.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		val = DecodeBool(v)
	case descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_UINT64:
		val = int64(v)
	case descriptor.FieldDescriptorProto_TYPE_SINT32,
		descriptor.FieldDescriptorProto_TYPE_SINT64:
		val = int64(DecodeZigZag(v))
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		val = int64(v)
	default:
		dv.err = multierror.Append(dv.err, fmt.Errorf("unexpected field type %q for varint encoding", f.GetType()))
		return
	}
	dv.setValue(f, val)
}

func (dv *decodeVisitor) fixed32(n Number, v uint32) {
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
		val = math.Float32frombits(v)
	default:
		dv.err = multierror.Append(dv.err, fmt.Errorf("unexpected field type %q for fixed32 encoding", f.GetType()))
		return
	}
	dv.setValue(f, val)
}

func (dv *decodeVisitor) fixed64(n Number, v uint64) {
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
func (mv *mapVisitor) varint(Number, uint64)  {}
func (mv *mapVisitor) fixed32(Number, uint32) {}
func (mv *mapVisitor) fixed64(Number, uint64) {}
func (mv *mapVisitor) bytes(n Number, v []byte) {
	switch n {
	case mv.desc.Field[0].GetNumber():
		mv.key = string(v)
	case mv.desc.Field[1].GetNumber():
		mv.value = string(v)
	}
}

func (dv *decodeVisitor) bytes(n Number, v []byte) {
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
			if _, ok := dv.out[f.GetName()]; !ok {
				dv.out[f.GetName()] = make(map[string]string)
			}

			visitor := &mapVisitor{desc: mapType}
			for len(v) > 0 {
				_, _, m := ConsumeField(visitor, v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse map field %q: %v", f.GetName(), ParseError(m)))
					return
				}
				v = v[m:]
			}

			dv.out[f.GetName()].(map[string]string)[visitor.key] = visitor.value
			return
		}
		// TODO(kuat): implement sub-message decoding
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
				elt, m = ConsumeVarint(v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse packed varint field %q", f.GetName()))
					return
				}
				dv.varint(n, elt)

			case descriptor.FieldDescriptorProto_TYPE_FIXED32,
				descriptor.FieldDescriptorProto_TYPE_SFIXED32,
				descriptor.FieldDescriptorProto_TYPE_FLOAT:
				var elt uint32
				elt, m = ConsumeFixed32(v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse packed fixed32 field %q", f.GetName()))
					return
				}
				dv.fixed32(n, elt)

			case descriptor.FieldDescriptorProto_TYPE_FIXED64,
				descriptor.FieldDescriptorProto_TYPE_SFIXED64,
				descriptor.FieldDescriptorProto_TYPE_DOUBLE:
				var elt uint64
				elt, m = ConsumeFixed64(v)
				if m < 0 {
					dv.err = multierror.Append(dv.err, fmt.Errorf("failed to parse packed fixed64 field %q", f.GetName()))
					return
				}
				dv.fixed64(n, elt)

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

// wireVisitor is used to process field values on-line
type wireVisitor interface {
	varint(Number, uint64)
	fixed32(Number, uint32)
	fixed64(Number, uint64)
	bytes(Number, []byte)
}

// Below is the code borrowed from golang/protobuf api-v2 branch, intended to be aligned closely once
// the upstream makes a release. The following changes have been made:
// - add a wire visitor that calls back with the field values

// Number represents the field number.
type Number = int32

// Bounds for field numbers
const (
	MinValidNumber      Number = 1
	FirstReservedNumber Number = 19000
	LastReservedNumber  Number = 19999
	MaxValidNumber      Number = 1<<29 - 1
)

// Type represents the wire type.
type Type int8

// Wire types
const (
	VarintType     Type = 0
	Fixed32Type    Type = 5
	Fixed64Type    Type = 1
	BytesType      Type = 2
	StartGroupType Type = 3
	EndGroupType   Type = 4
)

const (
	_ = -iota
	errCodeTruncated
	errCodeFieldNumber
	errCodeOverflow
	errCodeReserved
	errCodeEndGroup
)

var (
	errFieldNumber = errors.New("invalid field number")
	errOverflow    = errors.New("variable length integer overflow")
	errReserved    = errors.New("cannot parse reserved wire type")
	errEndGroup    = errors.New("mismatching end group marker")
	errParse       = errors.New("parse error")
)

// ParseError converts an error code into an error value.
// This returns nil if n is a non-negative number.
func ParseError(n int) error {
	if n >= 0 {
		return nil
	}
	switch n {
	case errCodeTruncated:
		return io.ErrUnexpectedEOF
	case errCodeFieldNumber:
		return errFieldNumber
	case errCodeOverflow:
		return errOverflow
	case errCodeReserved:
		return errReserved
	case errCodeEndGroup:
		return errEndGroup
	default:
		return errParse
	}
}

// ConsumeField parses an entire field record (both tag and value) and returns
// the field number, the wire type, and the total length.
// This returns a negative length upon an error (see ParseError).
//
// The total length includes the tag header and the end group marker (if the
// field is a group).
//
// DELTA: a visitor is added that receives call backs with field values.
func ConsumeField(visitor wireVisitor, b []byte) (Number, Type, int) {
	num, typ, n := ConsumeTag(b)
	if n < 0 {
		return 0, 0, n // forward error code
	}
	m := ConsumeFieldValue(visitor, num, typ, b[n:])
	if m < 0 {
		return 0, 0, m // forward error code
	}
	return num, typ, n + m
}

// ConsumeFieldValue parses a field value and returns its length.
// This assumes that the field Number and wire Type have already been parsed.
// This returns a negative length upon an error (see ParseError).
//
// When parsing a group, the length includes the end group marker and
// the end group is verified to match the starting field number.
//
// DELTA: removed group support (deprecated feature)
func ConsumeFieldValue(visitor wireVisitor, num Number, typ Type, b []byte) int {
	switch typ {
	case VarintType:
		v, n := ConsumeVarint(b)
		visitor.varint(num, v)
		return n
	case Fixed32Type:
		v, n := ConsumeFixed32(b)
		visitor.fixed32(num, v)
		return n
	case Fixed64Type:
		v, n := ConsumeFixed64(b)
		visitor.fixed64(num, v)
		return n
	case BytesType:
		v, n := ConsumeBytes(b)
		visitor.bytes(num, v)
		return n
	default:
		return errCodeReserved
	}
}

// ConsumeTag parses b as a varint-encoded tag, reporting its length.
// This returns a negative length upon an error (see ParseError).
func ConsumeTag(b []byte) (Number, Type, int) {
	v, n := ConsumeVarint(b)
	if n < 0 {
		return 0, 0, n // forward error code
	}
	num, typ := DecodeTag(v)
	if num < MinValidNumber {
		return 0, 0, errCodeFieldNumber
	}
	return num, typ, n
}

// ConsumeVarint parses b as a varint-encoded uint64, reporting its length.
// This returns a negative length upon an error (see ParseError).
func ConsumeVarint(b []byte) (v uint64, n int) {
	// TODO: Specialize for sizes 1 and 2 with mid-stack inlining.
	var y uint64
	if len(b) <= 0 {
		return 0, errCodeTruncated
	}
	v = uint64(b[0])
	if v < 0x80 {
		return v, 1
	}
	v -= 0x80

	if len(b) <= 1 {
		return 0, errCodeTruncated
	}
	y = uint64(b[1])
	v += y << 7
	if y < 0x80 {
		return v, 2
	}
	v -= 0x80 << 7

	if len(b) <= 2 {
		return 0, errCodeTruncated
	}
	y = uint64(b[2])
	v += y << 14
	if y < 0x80 {
		return v, 3
	}
	v -= 0x80 << 14

	if len(b) <= 3 {
		return 0, errCodeTruncated
	}
	y = uint64(b[3])
	v += y << 21
	if y < 0x80 {
		return v, 4
	}
	v -= 0x80 << 21

	if len(b) <= 4 {
		return 0, errCodeTruncated
	}
	y = uint64(b[4])
	v += y << 28
	if y < 0x80 {
		return v, 5
	}
	v -= 0x80 << 28

	if len(b) <= 5 {
		return 0, errCodeTruncated
	}
	y = uint64(b[5])
	v += y << 35
	if y < 0x80 {
		return v, 6
	}
	v -= 0x80 << 35

	if len(b) <= 6 {
		return 0, errCodeTruncated
	}
	y = uint64(b[6])
	v += y << 42
	if y < 0x80 {
		return v, 7
	}
	v -= 0x80 << 42

	if len(b) <= 7 {
		return 0, errCodeTruncated
	}
	y = uint64(b[7])
	v += y << 49
	if y < 0x80 {
		return v, 8
	}
	v -= 0x80 << 49

	if len(b) <= 8 {
		return 0, errCodeTruncated
	}
	y = uint64(b[8])
	v += y << 56
	if y < 0x80 {
		return v, 9
	}
	v -= 0x80 << 56

	if len(b) <= 9 {
		return 0, errCodeTruncated
	}
	y = uint64(b[9])
	v += y << 63
	if y < 2 {
		return v, 10
	}
	return 0, errCodeOverflow
}

// ConsumeFixed32 parses b as a little-endian uint32, reporting its length.
// This returns a negative length upon an error (see ParseError).
func ConsumeFixed32(b []byte) (v uint32, n int) {
	if len(b) < 4 {
		return 0, errCodeTruncated
	}
	v = uint32(b[0])<<0 | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return v, 4
}

// ConsumeFixed64 parses b as a little-endian uint64, reporting its length.
// This returns a negative length upon an error (see ParseError).
func ConsumeFixed64(b []byte) (v uint64, n int) {
	if len(b) < 8 {
		return 0, errCodeTruncated
	}
	v = uint64(b[0])<<0 | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 | uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
	return v, 8
}

// SizeFixed64 returns the encoded size of a fixed64; which is always 8.
func SizeFixed64() int {
	return 8
}

// ConsumeBytes parses b as a length-prefixed bytes value, reporting its length.
// This returns a negative length upon an error (see ParseError).
func ConsumeBytes(b []byte) (v []byte, n int) {
	m, n := ConsumeVarint(b)
	if n < 0 {
		return nil, n // forward error code
	}
	if m > uint64(len(b[n:])) {
		return nil, errCodeTruncated
	}
	return b[n:][:m], n + int(m)
}

// DecodeTag decodes the field Number and wire Type from its unified form.
// The Number is -1 if the decoded field number overflows.
// Other than overflow, this does not check for field number validity.
func DecodeTag(x uint64) (Number, Type) {
	num := Number(x >> 3)
	if num > MaxValidNumber {
		num = -1
	}
	return num, Type(x & 7)
}

// DecodeZigZag decodes a zig-zag-encoded uint64 as an int64.
//	Input:  {…,  5,  3,  1,  0,  2,  4,  6, …}
//	Output: {…, -3, -2, -1,  0, +1, +2, +3, …}
func DecodeZigZag(x uint64) int64 {
	return int64(x>>1) ^ int64(x)<<63>>63
}

// DecodeBool decodes a uint64 as a bool.
//	Input:  {    0,    1,    2, …}
//	Output: {false, true, true, …}
func DecodeBool(x uint64) bool {
	return x != 0
}
