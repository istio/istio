// Copyright 2018 Istio Authors
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

// Package resource contains core abstract types for representing configuration resources.
package resource

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
)

// Collection of the resource.
type Collection struct{ string }

// TypeURL of the resource.
type TypeURL struct{ string }

// Version is the version identifier of a resource.
type Version string

// FullName of the resource. It is unique within a given set of resource of the same collection.
type FullName struct {
	string
}

// Key uniquely identifies a (mutable) config resource in the config space.
type Key struct {
	// Collection of the resource.
	Collection Collection

	// Fully qualified name of the resource.
	FullName FullName
}

// VersionedKey uniquely identifies a snapshot of a config resource in the config space.
type VersionedKey struct {
	Key
	Version Version
}

// Labels are a map of string keys and values that can be used to organize and categorize
// resources within a collection.
type Labels map[string]string

// Annotations are a map of string keys and values that can be used by source and sink to communicate
// arbitrary metadata about this resource.
type Annotations map[string]string

type Metadata struct {
	CreateTime  time.Time
	Labels      Labels
	Annotations Annotations
}

// Entry is the abstract representation of a versioned config resource in Istio.
type Entry struct {
	Metadata Metadata
	ID       VersionedKey
	Item     proto.Message
}

// Info is the type metadata for an Entry.
type Info struct {
	// Collection of the resource that this info is about
	Collection Collection

	goType  reflect.Type
	TypeURL TypeURL
}

// newTypeURL validates the passed in url as a type url, and returns a strongly typed version.
func newTypeURL(rawurl string) (TypeURL, error) {
	candidate, err := url.Parse(rawurl)
	if err != nil {
		return TypeURL{}, err
	}

	if candidate.Scheme != "" && candidate.Scheme != "http" && candidate.Scheme != "https" {
		return TypeURL{}, fmt.Errorf("only empty, http or https schemes are allowed: %q", candidate.Scheme)
	}

	parts := strings.Split(candidate.Path, "/")
	if len(parts) <= 1 || parts[len(parts)-1] == "" {
		return TypeURL{}, fmt.Errorf("invalid URL path: %q", candidate.Path)
	}

	return TypeURL{rawurl}, nil
}

// MessageName portion of the type URL.
func (t TypeURL) MessageName() string {
	parts := strings.Split(t.string, "/")
	return parts[len(parts)-1]
}

// String interface method implementation.
func (t TypeURL) String() string {
	return t.string
}

// newCollection returns a strongly typed collection.
func newCollection(collection string) Collection {
	return Collection{collection}
}

// String interface method implementation.
func (t Collection) String() string {
	return t.string
}

// FullNameFromNamespaceAndName returns a FullName from namespace and name.
func FullNameFromNamespaceAndName(namespace, name string) FullName {
	if namespace == "" {
		return FullName{string: name}
	}

	return FullName{string: namespace + "/" + name}
}

// String inteface implementation.
func (n FullName) String() string {
	return n.string
}

// InterpretAsNamespaceAndName tries to split the name as namespace and name
func (n FullName) InterpretAsNamespaceAndName() (string, string) {
	parts := strings.SplitN(n.string, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}

// String interface method implementation.
func (k Key) String() string {
	return fmt.Sprintf("[Key](%s:%s)", k.Collection, k.FullName)
}

// String interface method implementation.
func (k VersionedKey) String() string {
	return fmt.Sprintf("[VKey](%s:%s @%s)", k.Collection, k.FullName, k.Version)
}

// IsEmpty returns true if the resource Entry.Item is nil.
func (r *Entry) IsEmpty() bool {
	return r.Item == nil
}

// String interface method implementation.
func (i *Info) String() string {
	return fmt.Sprintf("[Info](%s,%s)", i.Collection, i.Collection)
}

// NewProtoInstance returns a new instance of the underlying proto for this resource.
func (i *Info) NewProtoInstance() proto.Message {

	instance := reflect.New(i.goType).Interface()

	if p, ok := instance.(proto.Message); !ok {
		panic(fmt.Sprintf(
			"NewProtoInstance: message is not an instance of proto.Message. kind:%s, type:%v, value:%v",
			i.Collection, i.goType, instance))
	} else {
		return p
	}
}
