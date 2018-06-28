//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Package resource contains core abstract types for representing configuration resources.
package resource

import (
	"fmt"
	"reflect"

	prgogo "github.com/gogo/protobuf/proto"
	prlang "github.com/golang/protobuf/proto"
)

// MessageName is the proto message name of a resource.
type MessageName string

// Version is the version identifier of a resource.
type Version string

// Key uniquely identifies a (mutable) config resource in the config space.
type Key struct {
	// MessageName of the resource.
	MessageName MessageName

	// Fully qualified name of the resource.
	FullName string
}

// VersionedKey uniquely identifies a snapshot of a config resource in the config space, at a given
// time.
type VersionedKey struct {
	Key
	Version Version
}

// Entry is the abstract representation of a versioned config resource in Istio.
type Entry struct {
	ID   VersionedKey
	Item prlang.Message
}

// Info is the type metadata for an Entry.
type Info struct {
	// The message name of the resource that this info is about
	MessageName MessageName

	// Indicates whether the proto is defined as Gogo.
	IsGogo bool

	// The Type URL to use, when encoding as Any
	TypeURL string
}

// String interface method implementation.
func (k Key) String() string {
	return fmt.Sprintf("[Key](%s:%s)", k.MessageName, k.FullName)
}

// String interface method implementation.
func (k VersionedKey) String() string {
	return fmt.Sprintf("[VKey](%s:%s @%s)", k.MessageName, k.FullName, k.Version)
}

// IsEmpty returns true if the resource Entry.Item is nil.
func (r *Entry) IsEmpty() bool {
	return r.Item == nil
}

// String interface method implementation.
func (i *Info) String() string {
	return fmt.Sprintf("[Info](%s,%s)", i.MessageName, i.TypeURL)
}

// NewProtoInstance returns a new instance of the underlying proto for this resource.
func (i *Info) NewProtoInstance() prlang.Message {
	return i.newProtoInstance(prlang.MessageType, prgogo.MessageType)
}

func (i *Info) newProtoInstance(fnLang, fnGogo func(string) reflect.Type) prlang.Message {
	var t reflect.Type

	if i.IsGogo {
		t = fnGogo(string(i.MessageName))
	} else {
		t = fnLang(string(i.MessageName))
	}

	if t == nil {
		panic(fmt.Sprintf("NewProtoInstance: unable to instantiate proto instance: %s", i.MessageName))
	}

	if t.Kind() != reflect.Ptr {
		panic(fmt.Sprintf("NewProtoInstance: type is not pointer: kind:%s, type:%v", i.MessageName, t))
	}
	t = t.Elem()

	instance := reflect.New(t).Interface()

	if p, ok := instance.(prlang.Message); !ok {
		panic(fmt.Sprintf(
			"NewProtoInstance: message is not an instance of proto.Message. kind:%s, type:%v, value:%v",
			i.MessageName, t, instance))
	} else {
		return p
	}
}
