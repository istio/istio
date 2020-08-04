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

package resource

import (
	"errors"
	"fmt"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pkg/config/schema/resource"
)

var scope = log.RegisterScope("resource", "Core resource model scope", 0)

// Serialize converts a resource entry into its enveloped form.
func Serialize(r *Instance) (*mcp.Resource, error) {

	a, err := types.MarshalAny(r.Message)
	if err != nil {
		scope.Errorf("Error serializing proto from source r: %v:", r)
		return nil, err
	}

	metadata, err := SerializeMetadata(r.Metadata)
	if err != nil {
		scope.Errorf("Error serializing metadata for event (%v): %v", r, err)
		return nil, err
	}

	entry := &mcp.Resource{
		Metadata: metadata,
		Body:     a,
	}

	return entry, nil
}

// MustSerialize converts a resource entry into its enveloped form or panics if it cannot.
func MustSerialize(r *Instance) *mcp.Resource {
	m, err := Serialize(r)
	if err != nil {
		panic(fmt.Sprintf("resource.MustSerialize: %v", err))
	}
	return m
}

// SerializeAll envelopes and returns all the entries.
func SerializeAll(resources []*Instance) ([]*mcp.Resource, error) {
	result := make([]*mcp.Resource, len(resources))
	for i, r := range resources {
		r, err := Serialize(r)
		if err != nil {
			return nil, err
		}
		result[i] = r
	}
	return result, nil
}

// SerializeMetadata converts the given metadata to its enveloped form.
func SerializeMetadata(m Metadata) (*mcp.Metadata, error) {
	createTime, err := types.TimestampProto(m.CreateTime)
	if err != nil {
		scope.Errorf("Error serializing resource create_time: %v", err)
		return nil, err
	}

	return &mcp.Metadata{
		Name:        m.FullName.String(),
		CreateTime:  createTime,
		Version:     string(m.Version),
		Annotations: m.Annotations,
		Labels:      m.Labels,
	}, nil
}

// Deserialize an entry from an envelope.
func Deserialize(e *mcp.Resource, s resource.Schema) (*Instance, error) {
	p, err := types.EmptyAny(e.Body)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling proto: %v", err)
	}

	metadata, err := DeserializeMetadata(e.Metadata, s)
	if err != nil {
		return nil, err
	}

	if err = types.UnmarshalAny(e.Body, p); err != nil {
		return nil, fmt.Errorf("error unmarshaling body: %v", err)
	}

	return &Instance{
		Metadata: metadata,
		Message:  p,
	}, nil
}

// MustDeserialize deserializes an entry from an envelope or panics.
func MustDeserialize(e *mcp.Resource, s resource.Schema) *Instance {
	m, err := Deserialize(e, s)
	if err != nil {
		panic(fmt.Sprintf("resource.MustDeserialize: %v", err))
	}
	return m
}

// DeserializeAll extracts all entries from the given envelopes and returns.
func DeserializeAll(es []*mcp.Resource, s resource.Schema) ([]*Instance, error) {
	result := make([]*Instance, len(es))
	for i, e := range es {
		r, err := Deserialize(e, s)
		if err != nil {
			return nil, err
		}
		result[i] = r
	}
	return result, nil
}

// DeserializeMetadata extracts metadata portion of the envelope
func DeserializeMetadata(m *mcp.Metadata, s resource.Schema) (Metadata, error) {
	if s == nil {
		return Metadata{}, errors.New("error unmarshaling metadata. Resource schema must not be nil")
	}

	createTime, err := types.TimestampFromProto(m.CreateTime)
	if err != nil {
		return Metadata{}, fmt.Errorf("error unmarshaling create time: %v", err)
	}

	name, err := ParseFullName(m.Name)
	if err != nil {
		return Metadata{}, fmt.Errorf("error unmarshaling name: %v", err)
	}

	return Metadata{
		FullName:    name,
		CreateTime:  createTime,
		Version:     Version(m.Version),
		Annotations: m.Annotations,
		Labels:      m.Labels,
		Schema:      s,
	}, nil
}
