// Copyright 2019 Istio Authors
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
	"fmt"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("resource", "Core resource model scope", 0)

// Serialize converts a resource entry into its enveloped form.
func Serialize(e *Entry) (*mcp.Resource, error) {

	a, err := types.MarshalAny(e.Item)
	if err != nil {
		scope.Errorf("Error serializing proto from source e: %v:", e)
		return nil, err
	}

	createTime, err := types.TimestampProto(e.Metadata.CreateTime)
	if err != nil {
		scope.Errorf("Error parsing resource create_time for event (%v): %v", e, err)
		return nil, err
	}

	entry := &mcp.Resource{
		Metadata: &mcp.Metadata{
			Name:        e.Metadata.Name.String(),
			CreateTime:  createTime,
			Version:     string(e.Metadata.Version),
			Annotations: e.Metadata.Annotations,
			Labels:      e.Metadata.Labels,
		},
		Body: a,
	}

	return entry, nil
}

// MustSerialize converts a resource entry into its enveloped form or panics if it cannot.
func MustSerialize(e *Entry) *mcp.Resource {
	m, err := Serialize(e)
	if err != nil {
		panic(fmt.Sprintf("resource.MustSerialize: %v", err))
	}
	return m
}

// SerializeAll envelopes and returns all the entries.
func SerializeAll(entries []*Entry) ([]*mcp.Resource, error) {
	result := make([]*mcp.Resource, len(entries))
	for i, e := range entries {
		r, err := Serialize(e)
		if err != nil {
			return nil, err
		}
		result[i] = r
	}
	return result, nil
}

// Deserialize an entry from an envelope.
func Deserialize(e *mcp.Resource) (*Entry, error) {
	p, err := types.EmptyAny(e.Body)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling proto: %v", err)
	}

	createTime, err := types.TimestampFromProto(e.Metadata.CreateTime)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling create time: %v", err)
	}

	if err = types.UnmarshalAny(e.Body, p); err != nil {
		return nil, fmt.Errorf("error unmarshaling body: %v", err)
	}

	return &Entry{
		Metadata: Metadata{
			Name:        Name{e.Metadata.Name},
			CreateTime:  createTime,
			Version:     Version(e.Metadata.Version),
			Annotations: e.Metadata.Annotations,
			Labels:      e.Metadata.Labels,
		},
		Item: p,
	}, nil
}

// MustDeserialize deserializes an entry from an envelope or panics.
func MustDeserialize(e *mcp.Resource) *Entry {
	m, err := Deserialize(e)
	if err != nil {
		panic(fmt.Sprintf("resource.MustDeserialize: %v", err))
	}
	return m
}

// DeserializeAll extracts all entries from the given envelopes and returns.
func DeserializeAll(es []*mcp.Resource) ([]*Entry, error) {
	result := make([]*Entry, len(es))
	for i, e := range es {
		r, err := Deserialize(e)
		if err != nil {
			return nil, err
		}
		result[i] = r
	}
	return result, nil
}
