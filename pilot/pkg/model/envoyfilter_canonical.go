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

package model

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
)

const anyMessageFullName = "google.protobuf.Any"

// canonicalizeAnys rewrites the payload of every nested google.protobuf.Any in m using
// deterministic serialization, so that the same input configuration always produces
// byte-identical output in every process.
//
// Background: an EnvoyFilter patch value is a structpb.Struct, that is a Go map.
// Converting it to a typed proto goes through protobuf-go's jsonpb, which stores nested
// Any payloads using a plain proto.Marshal. Go randomizes map iteration order, so two
// pilot replicas (and even two PushContext rebuilds in the same process) can produce
// different bytes for the very same configuration.
//
// Envoy decides whether a config update is needed by hashing the proto
// (MessageUtil::hash in ListenerManagerImpl::addOrUpdateListenerInternal). Diffs in map
// ordering are visible to that hash, so a gateway that reconnects to a different pilot
// replica sees a "changed" listener and takes the in-place filter chain update path.
// That path rebuilds and drains filter chains, and once the drain expires it closes the
// connections bound to them with response code detail
// "filter_chain_is_being_removed" - killing in-flight requests.
//
// Deterministic marshaling sorts map keys, so the output becomes a pure function of the
// message content. Note that the outer serialization (protoconv.MessageToAny, used when
// the patch is inserted into a listener) is already deterministic, but it cannot see
// through Any.value, which is an opaque bytes field; that is exactly why the nested
// payloads have to be rewritten here.
func canonicalizeAnys(m proto.Message) {
	if m == nil {
		return
	}
	r := m.ProtoReflect()
	if !r.IsValid() {
		return
	}
	canonicalizeMessage(r)
}

// canonicalizeMessage walks every field of m, recursing into submessages, list elements
// and map values, and canonicalizes whatever it finds.
func canonicalizeMessage(m protoreflect.Message) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
			if fd.MapValue().Kind() != protoreflect.MessageKind {
				return true
			}
			v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
				if mv.Message().IsValid() {
					canonicalizeMessage(mv.Message())
				}
				return true
			})
		case fd.IsList():
			if fd.Kind() != protoreflect.MessageKind {
				return true
			}
			l := v.List()
			for i := 0; i < l.Len(); i++ {
				if e := l.Get(i).Message(); e.IsValid() {
					canonicalizeMessage(e)
				}
			}
		case fd.Kind() == protoreflect.MessageKind:
			sub := v.Message()
			if !sub.IsValid() {
				return true
			}
			if sub.Descriptor().FullName() == anyMessageFullName {
				canonicalizeAny(sub)
				return true
			}
			canonicalizeMessage(sub)
		}
		return true
	})
}

// canonicalizeAny re-packs a single Any with deterministic serialization. Payloads whose
// type is not resolvable are left untouched: the configuration is still valid, it just
// is not byte-stable, and dropping the payload would silently change behavior.
func canonicalizeAny(m protoreflect.Message) {
	a, ok := m.Interface().(*anypb.Any)
	if !ok || a == nil || len(a.Value) == 0 {
		return
	}
	inner, err := a.UnmarshalNew()
	if err != nil {
		return
	}
	canonicalizeMessage(inner.ProtoReflect())

	canonical := &anypb.Any{}
	if err := anypb.MarshalFrom(canonical, inner, proto.MarshalOptions{Deterministic: true}); err != nil {
		return
	}
	// MarshalFrom derives the type URL from the message, keep the original one: EnvoyFilter
	// patches may use a type URL that differs from the Go type's default (for example an
	// older API version) and Envoy has to receive exactly what was configured.
	canonical.TypeUrl = a.TypeUrl
	a.TypeUrl, a.Value = canonical.TypeUrl, canonical.Value
}
