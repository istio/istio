//  Copyright 2018 Istio Authors
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

// TypeURL of the resource.
type TypeURL struct{ string }

// Version is the version identifier of a resource.
type Version string

// Key uniquely identifies a (mutable) config resource in the config space.
type Key struct {
	// TypeURL of the resource.
	TypeURL TypeURL

	// Fully qualified name of the resource.
	FullName string
}

// VersionedKey uniquely identifies a snapshot of a config resource in the config space, at a given
// time.
type VersionedKey struct {
	Key
	Version    Version
	CreateTime time.Time
}

// Entry is the abstract representation of a versioned config resource in Istio.
type Entry struct {
	ID   VersionedKey
	Item proto.Message
}

// Info is the type metadata for an Entry.
type Info struct {
	// TypeURL of the resource that this info is about
	TypeURL TypeURL

	goType reflect.Type
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

// String interface method implementation.
func (k Key) String() string {
	return fmt.Sprintf("[Key](%s:%s)", k.TypeURL, k.FullName)
}

// String interface method implementation.
func (k VersionedKey) String() string {
	return fmt.Sprintf("[VKey](%s:%s @%s)", k.TypeURL, k.FullName, k.Version)
}

// IsEmpty returns true if the resource Entry.Item is nil.
func (r *Entry) IsEmpty() bool {
	return r.Item == nil
}

// String interface method implementation.
func (i *Info) String() string {
	return fmt.Sprintf("[Info](%s,%s)", i.TypeURL, i.TypeURL)
}

// NewProtoInstance returns a new instance of the underlying proto for this resource.
func (i *Info) NewProtoInstance() proto.Message {

	instance := reflect.New(i.goType).Interface()

	if p, ok := instance.(proto.Message); !ok {
		panic(fmt.Sprintf(
			"NewProtoInstance: message is not an instance of proto.Message. kind:%s, type:%v, value:%v",
			i.TypeURL, i.goType, instance))
	} else {
		return p
	}
}
