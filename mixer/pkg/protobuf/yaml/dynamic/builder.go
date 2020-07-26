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

package dynamic

import (
	"fmt"
	"sort"

	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/runtime/lang"
	istiolog "istio.io/pkg/log"
)

var (
	builderLog = istiolog.RegisterScope("grpcAdapter", "dynamic proto encoder debugging", 0)
)

type (

	// Builder builds encoder based on data
	Builder struct {
		resolver    yaml.Resolver
		compiler    lang.Compiler
		skipUnknown bool
		namedTypes  map[string]NamedEncoderBuilderFunc
	}

	// NamedEncoderBuilderFunc funcs have a special way to process input for encoding specific types
	// for example istio...Value field accepts user input in a specific way
	NamedEncoderBuilderFunc func(m *descriptor.DescriptorProto, fd *descriptor.FieldDescriptorProto, v interface{}, compiler lang.Compiler) (Encoder, error)
)

// NewEncoderBuilder creates an EncoderBuilder.
func NewEncoderBuilder(resolver yaml.Resolver, compiler lang.Compiler, skipUnknown bool) *Builder {
	return &Builder{
		resolver:    resolver,
		compiler:    compiler,
		skipUnknown: skipUnknown,
		namedTypes:  valueTypeBuilderFuncs(),
	}
}

// BuildWithLength builds an Encoder that also encodes length of the top level message.
func (c Builder) BuildWithLength(msgName string, data map[string]interface{}) (Encoder, error) {
	m := c.resolver.ResolveMessage(msgName)
	if m == nil {
		return nil, fmt.Errorf("cannot resolve message '%s'", msgName)
	}

	return c.buildMessage(m, data, false)
}

// Build builds an Encoder
func (c Builder) Build(msgName string, data map[string]interface{}) (Encoder, error) {
	m := c.resolver.ResolveMessage(msgName)
	if m == nil {
		return nil, fmt.Errorf("cannot resolve message '%s'", msgName)
	}

	return c.buildMessage(m, data, true)
}

func (c Builder) buildMessage(md *descriptor.DescriptorProto, data map[string]interface{}, skipEncodeLength bool) (Encoder, error) {
	me := messageEncoder{
		skipEncodeLength: skipEncodeLength,
	}
	isMap := md.GetOptions().GetMapEntry()

	// sort keys so that processing is done in a deterministic order.
	// It makes logs deterministic.
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		var err error

		v := data[k]
		fd := yaml.FindFieldByName(md, k)
		if fd == nil {
			if c.skipUnknown {
				builderLog.Debugf("skipping key=%s from message %s", k, md.GetName())
				continue
			}
			return nil, fmt.Errorf("fieldEncoder '%s' not found in message '%s'", k, md.GetName())
		}

		// If the input already has an encoder
		// just use it
		if de, ok := v.(Encoder); ok {
			fld := makeField(fd)
			fld.encoder = []Encoder{de}
			me.fields = append(me.fields, fld)
			continue
		}

		// if the message is a map, key is never
		// treated as an expression.
		noExpr := isMap && k == "key"

		switch fd.GetType() {
		case descriptor.FieldDescriptorProto_TYPE_STRING:
			var ma []interface{}
			var ok bool
			if fd.IsRepeated() {
				if ma, ok = v.([]interface{}); !ok {
					return nil, fmt.Errorf("unable to process %s:  %v, got %T, want: []interface{}", fd.GetName(), v, v)
				}
			} else {
				ma = []interface{}{v}
			}

			for _, vv := range ma {
				var fld *fieldEncoder
				if fld, err = c.buildPrimitiveField(vv, fd, noExpr); err != nil {
					return nil, fmt.Errorf("unable to encode fieldEncoder %s: %v. %v", fd.GetName(), v, err)
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

			var fld *fieldEncoder
			if fld, err = c.buildPrimitiveField(v, fd, noExpr); err != nil {
				return nil, fmt.Errorf("unable to encode fieldEncoder: %v. %v", fd, err)
			}
			me.fields = append(me.fields, fld)
		case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
			m := c.resolver.ResolveMessage(fd.GetTypeName())
			if m == nil {
				return nil, fmt.Errorf("unable to resolve message '%s'", fd.GetTypeName())
			}

			var ma []interface{}
			var ok bool
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
			return nil, fmt.Errorf("field type not supported '%v'", fd.GetType())
		}
	}

	// sorting fields is recommended
	sort.SliceStable(me.fields, func(i, j int) bool {
		return me.fields[i].number < me.fields[j].number
	})

	return me, nil
}

// buildStaticEncoder builds static encoder with special case for enum.
func buildStaticEncoder(val interface{}, fd *descriptor.FieldDescriptorProto, e *descriptor.EnumDescriptorProto) (Encoder, error) {
	var enc Encoder
	var err error

	if fd.IsEnum() {
		pe := &staticEncoder{name: fd.GetName()}
		// EncodeEnum accepts strings and ints.
		pe.encodedData, err = EncodeEnum(val, pe.encodedData, e.Value)
		enc = pe
	} else {
		enc, err = BuildPrimitiveEncoder(val, fd)
	}

	return enc, err
}

// buildDynamicEncoder builds dynamic encoder with special case for enum.
func buildDynamicEncoder(sval string, fd *descriptor.FieldDescriptorProto, e *descriptor.EnumDescriptorProto, c lang.Compiler) (Encoder, error) {
	var err error

	var expr compiled.Expression
	var vt v1beta1.ValueType

	if expr, vt, err = c.Compile(sval); err != nil {
		return nil, err
	}

	if fd.IsEnum() {
		// enums can be expressed as String or INT only
		if !(vt == v1beta1.INT64 || vt == v1beta1.STRING) {
			return nil, fmt.Errorf("field:%v enum %v is of type:%v. want INT64 or STRING", fd.GetName(), sval, vt)
		}

		return &eEnumEncoder{
			expr:       expr,
			enumValues: e.Value,
		}, nil
	}

	return BuildPrimitiveEvalEncoder(expr, vt, fd)
}

// buildPrimitiveField handles encoding of single and repeated packed primitives
// if noExpr is true create static encoder.
func (c Builder) buildPrimitiveField(v interface{}, fd *descriptor.FieldDescriptorProto, noExpr bool) (*fieldEncoder, error) {
	fld := makeField(fd)
	enum := c.resolver.ResolveEnum(fd.GetTypeName())
	if fd.IsEnum() && enum == nil {
		return nil, fmt.Errorf("unable to resolve enum: %v for field %v", fd.GetTypeName(), fd.GetName())
	}

	va, packed := v.([]interface{})
	if !packed {
		va = []interface{}{v}
	}

	for _, vl := range va {
		val, isConstString := transformQuotedString(vl)
		sval, isString := val.(string)

		var err error
		var enc Encoder

		// if compiler is nil, everything is considered noExpr
		if noExpr || isConstString || !isString || c.compiler == nil {
			enc, err = buildStaticEncoder(val, fd, enum)
		} else {
			enc, err = buildDynamicEncoder(sval, fd, enum, c.compiler)
		}

		if err != nil {
			return nil, fmt.Errorf("unable to build primitive encoder for:%v %v. %v", fld.name, val, err)
		}

		// now enc != nil
		fld.encoder = append(fld.encoder, enc)
	}

	return fld, nil
}

func (c Builder) buildMessageField(ma []interface{}, m *descriptor.DescriptorProto,
	fd *descriptor.FieldDescriptorProto, me *messageEncoder) error {

	namedBuilder := c.namedTypes[fd.GetTypeName()]

	for _, vv := range ma {
		var de Encoder
		var err error

		if namedBuilder != nil {
			de, err = namedBuilder(m, fd, vv, c.compiler)
		} else {
			var ok bool
			var vq map[string]interface{}
			if vq, ok = vv.(map[string]interface{}); !ok {
				return fmt.Errorf("unable to process: %v, got %T, want: map[string]interface{}", fd, vv)
			}

			de, err = c.buildMessage(m, vq, false)
		}

		if err != nil {
			return fmt.Errorf("unable to build message field: %v, %v", fd.GetName(), err)
		}

		fld := makeField(fd)
		fld.encoder = []Encoder{de}
		me.fields = append(me.fields, fld)
	}
	return nil
}

// convertMapToMapentry converts {k:v} into { "key": k, "value": v }
func convertMapToMapentry(data interface{}) ([]interface{}, error) {
	md, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("incorrect map type:%T, want:map[string]interface{}", data)
	}
	res := make([]interface{}, 0, len(md))
	for k, v := range md {
		res = append(res, map[string]interface{}{
			"key":   k,
			"value": v,
		})
	}
	return res, nil
}
