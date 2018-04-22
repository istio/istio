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

package dynamic

import (
	"fmt"
	"sort"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/protobuf/yaml"
)

type (

	// Compiler compiles expression and returns a value type.
	Compiler interface {
		// Compile type Compiler interface {compiles expression and returns a value type.
		Compile(text string) (compiled.Expression, v1beta1.ValueType, error)
	}

	// Builder builds encoder based on data
	Builder struct {
		msgName     string
		resolver    yaml.Resolver
		data        map[interface{}]interface{}
		compiler    Compiler
		skipUnknown bool
	}
)

// NewEncoderBuilder creates an Builder.
func NewEncoderBuilder(msgName string, resolver yaml.Resolver, data map[interface{}]interface{},
	compiler compiled.Compiler, skipUnknown bool) *Builder {
	return &Builder{
		msgName:     msgName,
		resolver:    resolver,
		data:        data,
		compiler:    compiler,
		skipUnknown: skipUnknown}
}

// Build builds a DynamicEncoder
func (c Builder) Build() (Encoder, error) {
	m := c.resolver.ResolveMessage(c.msgName)
	if m == nil {
		return nil, fmt.Errorf("cannot resolve message '%s'", c.msgName)
	}

	return c.buildMessage(m, c.data, true)
}

func (c Builder) buildMessage(md *descriptor.DescriptorProto, data map[interface{}]interface{}, skipEncodeLength bool) (Encoder, error) {
	var err error
	var ok bool

	me := messageEncoder{
		skipEncodeLength: skipEncodeLength,
	}
	isMap := md.GetOptions().GetMapEntry()

	keys := make([]string, 0, len(data))
	for kk := range data {
		var k string
		if k, ok = kk.(string); !ok {
			return nil, fmt.Errorf("error processing message '%s':%v got %T want string", md.GetName(), kk, kk)
		}
		keys = append(keys, k)
	}
	// Ensure consistent log output.
	sort.Strings(keys)

	for _, k := range keys {
		v := data[k]
		fd := yaml.FindFieldByName(md, k)
		if fd == nil {
			if c.skipUnknown {
				continue
			}
			return nil, fmt.Errorf("field '%s' not found in message '%s'", k, md.GetName())
		}

		switch fd.GetType() {
		case descriptor.FieldDescriptorProto_TYPE_STRING:
			var ma []interface{}
			if fd.IsRepeated() {
				if ma, ok = v.([]interface{}); !ok {
					return nil, fmt.Errorf("unable to process: %v, got %T, want: []interface{}", fd, v)
				}
			} else {
				ma = []interface{}{v}
			}

			for _, vv := range ma {
				var fld *field
				if fld, err = c.buildPrimitiveField(vv, fd, isMap && k == "key"); err != nil {
					return nil, fmt.Errorf("unable to encode field: %v. %v", fd, err)
				}
				me.fields = append(me.fields, fld)
			}

			// primitives
		case descriptor.FieldDescriptorProto_TYPE_DOUBLE,
			descriptor.FieldDescriptorProto_TYPE_FLOAT,
			descriptor.FieldDescriptorProto_TYPE_INT64,
			descriptor.FieldDescriptorProto_TYPE_UINT64,
			descriptor.FieldDescriptorProto_TYPE_INT32,
			descriptor.FieldDescriptorProto_TYPE_FIXED64,
			descriptor.FieldDescriptorProto_TYPE_FIXED32,
			descriptor.FieldDescriptorProto_TYPE_BOOL,
			descriptor.FieldDescriptorProto_TYPE_UINT32,
			descriptor.FieldDescriptorProto_TYPE_SFIXED32,
			descriptor.FieldDescriptorProto_TYPE_SFIXED64,
			descriptor.FieldDescriptorProto_TYPE_SINT32,
			descriptor.FieldDescriptorProto_TYPE_SINT64,
			descriptor.FieldDescriptorProto_TYPE_ENUM:

			packed := fd.IsPacked() || fd.IsPacked3()
			if fd.IsRepeated() && !packed {
				return nil, fmt.Errorf("unpacked primitives not supported: %v", fd)
			}

			var fld *field
			if fld, err = c.buildPrimitiveField(v, fd, isMap && k == "key"); err != nil {
				return nil, fmt.Errorf("unable to encode field: %v. %v", fd, err)
			}
			me.fields = append(me.fields, fld)
		case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
			m := c.resolver.ResolveMessage(fd.GetTypeName())
			if m == nil {
				return nil, fmt.Errorf("unable to resolve message '%s'", fd.GetTypeName())
			}

			var ma []interface{}
			if m.GetOptions().GetMapEntry() { // this is a Map
				ma, err = convertMapToMapentry(v)
				if err != nil {
					return nil, fmt.Errorf("unable to process: %v, %v", fd, err)
				}
			} else if fd.IsRepeated() { // map entry is always repeated.
				ma, ok = v.([]interface{})
				if !ok {
					return nil, fmt.Errorf("unable to process: %v, got %T, want: []interface{}", fd, v)
				}
			} else {
				ma = []interface{}{v}
			}

			// now maps, messages and repeated messages all look the same.
			if err = c.buildMessageField(ma, m, fd, &me); err != nil {
				return nil, err
			}

		default:
			return nil, fmt.Errorf("field not supported '%v'", fd)
		}
	}

	// sorting fields is recommended
	sort.Slice(me.fields, func(i, j int) bool {
		return me.fields[i].number < me.fields[j].number
	})

	return me, nil
}

// buildPrimitiveField handles encoding of single and repeated packed primitives
func (c Builder) buildPrimitiveField(v interface{}, fd *descriptor.FieldDescriptorProto,
	static bool) (*field, error) {
	fld := makeField(fd)
	e := c.resolver.ResolveEnum(fd.GetTypeName())

	va, packed := v.([]interface{})
	if !packed {
		va = []interface{}{v}
	}

	for _, vl := range va {
		var err error

		val, isConstString := transFormQuotedString(vl)
		sval, isString := val.(string)

		var enc Encoder

		// if compiler is nil, everything is considered static
		if static || isConstString || !isString || c.compiler == nil {

			if fd.IsEnum() {
				pe := &staticEncoder{name: fd.GetName()}
				pe.encodedData, err = EncodeEnum(val, pe.encodedData, e.Value)
				enc = pe
			} else {
				enc, err = BuildPrimitiveEncoder(val, fd)
			}

			if err != nil {
				return nil, fmt.Errorf("unable to build primitive encoder: %v. %v", val, err)
			}
		} else {
			var expr compiled.Expression
			var vt v1beta1.ValueType

			if expr, vt, err = c.compiler.Compile(sval); err != nil {
				return nil, err
			}

			if fd.IsEnum() {
				// enums can be expressed as String or INT only
				if !(vt == v1beta1.INT64 || vt == v1beta1.STRING) {
					return nil, fmt.Errorf("unable to build eval encoder: enum %v for expression type:%v", val, vt)
				}
				enc = &eEnumEncoder{
					expr:       expr,
					enumValues: e.Value,
				}
			} else {
				enc, err = BuildPrimitiveEvalEncoder(expr, vt, fd)
			}

			if err != nil {
				return nil, fmt.Errorf("unable to build eval encoder: %v. %v", val, err)
			}
		}

		// ASSERT(enc != nil)
		fld.encoder = append(fld.encoder, enc)
	}

	return fld, nil
}

func (c Builder) buildMessageField(ma []interface{}, m *descriptor.DescriptorProto,
	fd *descriptor.FieldDescriptorProto, me *messageEncoder) error {

	for _, vv := range ma {
		var ok bool

		var vq map[interface{}]interface{}
		if vq, ok = vv.(map[interface{}]interface{}); !ok {
			return fmt.Errorf("unable to process: %v, got %T, want: map[string]interface{}", fd, vv)
		}

		var de Encoder
		var err error
		if de, err = c.buildMessage(m, vq, false); err != nil {
			return fmt.Errorf("unable to process: %v, %v", fd, err)
		}
		fld := makeField(fd)
		fld.encoder = []Encoder{de}
		me.fields = append(me.fields, fld)
	}

	return nil
}

// convertMapToMapentry converts {k:v} into { "key": k, "value": v }
func convertMapToMapentry(data interface{}) ([]interface{}, error) {
	md, ok := data.(map[interface{}]interface{})
	if !ok {
		return nil, fmt.Errorf("incorrect map type:%T, want:map[interface{}]interface{}", data)
	}
	res := make([]interface{}, 0, len(md))
	for k, v := range md {
		res = append(res, map[interface{}]interface{}{
			"key":   k,
			"value": v,
		})
	}
	return res, nil
}
