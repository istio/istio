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

package merge

/*
 CODE Copied and modified from https://github.com/kumahq/kuma/blob/master/pkg/util/proto/google_proto.go
 because of: https://github.com/golang/protobuf/issues/1359

  Copyright 2019 The Go Authors. All rights reserved.
  Use of this source code is governed by a BSD-style
  license that can be found in the LICENSE file.
*/

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"istio.io/istio/pkg/log"
)

type (
	MergeFunction func(mo mergeOptions, dst, src protoreflect.Message) protoreflect.Message
	mergeOptions  struct {
		customMergeFn map[protoreflect.FullName]MergeFunction
	}
)
type OptionFn func(options mergeOptions) mergeOptions

func MergeFunctionOptionFn(name protoreflect.FullName, function MergeFunction) OptionFn {
	return func(options mergeOptions) mergeOptions {
		options.customMergeFn[name] = function
		return options
	}
}

// ReplaceMergeFn instead of merging all subfields one by one, returns src
var ReplaceMergeFn MergeFunction = func(mo mergeOptions, dst, src protoreflect.Message) protoreflect.Message {
	log.Errorf("howardjohn: CALL REPLACE")
	// we can return src directly because this is a replace
	return src
}

// ReplaceMergeFn instead of merging all subfields one by one, returns src
var AnyMergeFn MergeFunction = func(mo mergeOptions, dst, src protoreflect.Message) protoreflect.Message {
	// we can return src directly because this is a replace
	tu := dst.Descriptor().Fields().ByName("type_url")
	v := dst.Descriptor().Fields().ByName("value")
	dt := dst.Get(tu).String()
	st := src.Get(tu).String()
	dv := dst.Get(v).Bytes()
	sv := src.Get(v).Bytes()
	log.Errorf("howardjohn: merge %v into %v", dt, st)
	log.Errorf("howardjohn: merge %+v %T %T", dst.Get(v).Interface(), dv, sv)
	_ = v
	log.Errorf("howardjohn: ANY MERGE %v / %+v", dst.Descriptor().FullName(), dst.Interface())
	if dt != st {
		// not the same type, just overwrite entirely
		return src
	}
	// They are the same type! we want to deep merge

	t, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(strings.TrimPrefix(dt, "type.googleapis.com/")))
	if err != nil || t == nil {
		panic(fmt.Sprintf("failed to find reflect type %q", protoreflect.FullName(dt))) // TODO
	}
	dun := t.New().Interface()
	dany := dst.Interface().(*anypb.Any)
	if err := dany.UnmarshalTo(dun); err != nil {
		panic(err.Error())
	}
	sun := t.New().Interface()
	sany := src.Interface().(*anypb.Any)
	if err := sany.UnmarshalTo(sun); err != nil {
		panic(err.Error())
	}
	log.Errorf("howardjohn: pre %+v", dun)
	mo.mergeMessage(dun.ProtoReflect(), sun.ProtoReflect())
	log.Errorf("howardjohn: post %+v", dun)

	//dun.MarshalFrom()
	a, err := MessageToAnyWithError(dun)
	if err != nil {
		panic(err.Error())
	}
	return  a.ProtoReflect()
}


// MessageToAnyWithError converts from proto message to proto Any
func MessageToAnyWithError(msg proto.Message) (*anypb.Any, error) {
	b, err := marshal(msg)
	if err != nil {
		return nil, err
	}
	return &anypb.Any{
		// nolint: staticcheck
		TypeUrl: "type.googleapis.com/" + string(msg.ProtoReflect().Descriptor().FullName()),
		Value:   b,
	}, nil
}

func marshal(msg proto.Message) ([]byte, error) {
	// If not available, fallback to normal implementation
	return proto.MarshalOptions{Deterministic: true}.Marshal(msg)
}

var options = []OptionFn{
	// Workaround https://github.com/golang/protobuf/issues/1359, merge duration properly
	MergeFunctionOptionFn((&durationpb.Duration{}).ProtoReflect().Descriptor().FullName(), ReplaceMergeFn),
	MergeFunctionOptionFn((&anypb.Any{}).ProtoReflect().Descriptor().FullName(), AnyMergeFn),
}

func Merge(dst, src proto.Message) {
	merge(dst, src, options...)
}

// Merge Code of proto.Merge with modifications to support custom types
func merge(dst, src proto.Message, opts ...OptionFn) {
	mo := mergeOptions{customMergeFn: map[protoreflect.FullName]MergeFunction{}}
	for _, opt := range opts {
		mo = opt(mo)
	}
	for c := range mo.customMergeFn {
		log.Errorf("howardjohn: have %v", c)
	}
	dstMsg, srcMsg := dst.ProtoReflect(), src.ProtoReflect()
	if dstMsg.Descriptor() != srcMsg.Descriptor() {
		if got, want := dstMsg.Descriptor().FullName(), srcMsg.Descriptor().FullName(); got != want {
			panic(fmt.Sprintf("descriptor mismatch: %v != %v", got, want))
		}
		panic("descriptor mismatch")
	}
	mo.mergeMessage(dstMsg, srcMsg)
}

func (o mergeOptions) mergeMessage(dst, src protoreflect.Message) {
	// The regular proto.mergeMessage would have a fast path method option here.
	// As we want to have exceptions we always use the slow path.
	if !dst.IsValid() {
		panic(fmt.Sprintf("cannot merge into invalid %v message", dst.Descriptor().FullName()))
	}
	mergeFn, exists := o.customMergeFn[dst.Descriptor().FullName()]
	if exists {
		nv := mergeFn(o, dst, src)
		for fn :=  range dst.Descriptor().Fields().Len() {
			fd := dst.Descriptor().Fields().Get(fn)
			dst.Clear(fd)
			log.Errorf("howardjohn: set %v to %v", fd.FullName(), nv.Get(fd))
			dst.Set(fd, nv.Get(fd))
		}
		return
	}

	src.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		log.Errorf("howardjohn: range over message %v %v", fd.FullName(), v)
		switch {
		case fd.IsList():
			o.mergeList(dst.Mutable(fd).List(), v.List(), fd)
		case fd.IsMap():
			o.mergeMap(dst.Mutable(fd).Map(), v.Map(), fd.MapValue())
		case fd.Message() != nil:
			log.Errorf("howardjohn: check method %v", fd.Message().FullName())
			o.mergeMaybeCustom(fd, v, dst)
		case fd.Kind() == protoreflect.BytesKind:
			dst.Set(fd, o.cloneBytes(v))
		default:
			dst.Set(fd, v)
		}
		return true
	})

	if len(src.GetUnknown()) > 0 {
		dst.SetUnknown(append(dst.GetUnknown(), src.GetUnknown()...))
	}
}

func (o mergeOptions) mergeMaybeCustom(fd protoreflect.FieldDescriptor, v protoreflect.Value, dst protoreflect.Message) {
	mergeFn, exists := o.customMergeFn[fd.Message().FullName()]
	if exists {
		dstV := mergeFn(o, dst.Mutable(fd).Message(), v.Message())
		dst.Set(fd, protoreflect.ValueOf(dstV))
	} else {
		o.mergeMessage(dst.Mutable(fd).Message(), v.Message())
	}
}

func (o mergeOptions) mergeMaybeCustom2(fd protoreflect.FieldDescriptor, v protoreflect.Value, dst protoreflect.Message) {
	o.mergeMessage(dst.Mutable(fd).Message(), v.Message())
	//mergeFn, exists := o.customMergeFn[fd.FullName()]
	//if exists {
	//	dstV := mergeFn(dst.Mutable(fd).Message(), v.Message())
	//	dst.Set(fd, protoreflect.ValueOf(dstV))
	//} else {
	//	o.mergeMessage(dst.Mutable(fd).Message(), v.Message())
	//}
}

func (o mergeOptions) mergeList(dst, src protoreflect.List, fd protoreflect.FieldDescriptor) {
	// Merge semantics appends to the end of the existing list.
	for i, n := 0, src.Len(); i < n; i++ {
		switch v := src.Get(i); {
		case fd.Message() != nil:
			log.Errorf("howardjohn: check method list %v", fd.Message().FullName())
			dstv := dst.NewElement()
			o.mergeMaybeCustom(fd, v, dstv.Message())
			dst.Append(dstv)
		case fd.Kind() == protoreflect.BytesKind:
			dst.Append(o.cloneBytes(v))
		default:
			dst.Append(v)
		}
	}
}

func (o mergeOptions) mergeMap(dst, src protoreflect.Map, fd protoreflect.FieldDescriptor) {
	// Merge semantics replaces, rather than merges into existing entries.
	src.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		switch {
		case fd.Message() != nil:
			log.Errorf("howardjohn: check method map %v", fd.Message().FullName())
			dstv := dst.Get(k)
			if !dstv.IsValid() {
				dstv = dst.NewValue()
			}
			o.mergeMessage(dstv.Message(), v.Message())
			dst.Set(k, dstv)
			//dstv := dst.NewValue()
			//log.Errorf("howardjohn: run new value %v / %v / %v / %v", dstv, dstv.Message(), dstv.Message().Type(), dstv.Message().Descriptor().FullName())
			//log.Errorf("howardjohn: run new value  fd %v / %v/%v", fd, fd.FullName(), fd.Message().FullName())
			//
			//ff := dstv.Message().Descriptor().Fields().ByName("value")
			//log.Errorf("howardjohn: run new value ff %v / %v", ff, ff.FullName())
			//o.mergeMaybeCustom2(ff, v, dstv.Message())
			//dst.Set(k, dstv)
		case fd.Kind() == protoreflect.BytesKind:
			dst.Set(k, o.cloneBytes(v))
		default:
			dst.Set(k, v)
		}
		return true
	})
}

func (o mergeOptions) cloneBytes(v protoreflect.Value) protoreflect.Value {
	return protoreflect.ValueOfBytes(append([]byte{}, v.Bytes()...))
}
