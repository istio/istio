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

package resource

import (
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("resource", "Core resource model scope", 0)

// ToMcpResource converts a resource entry into its enveloped form.
func ToMcpResource(e Entry) (*mcp.Resource, error) {

	serialized, err := proto.Marshal(e.Item)
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
			Name:        e.ID.FullName.String(),
			CreateTime:  createTime,
			Version:     string(e.ID.Version),
			Annotations: e.Metadata.Annotations,
			Labels:      e.Metadata.Labels,
		},
		Body: &types.Any{
			TypeUrl: e.ID.TypeURL.String(),
			Value:   serialized,
		},
	}

	return entry, nil
}

// ToMcpResourceAll envelopes and returns all the entries.
func ToMcpResourceAll(entries []Entry) ([]*mcp.Resource, error) {
	result := make([]*mcp.Resource, len(entries))
	for i, e := range entries {
		r, err := ToMcpResource(e)
		if err != nil {
			return nil, err
		}
		result[i] = r
	}
	return result, nil
}

// FromMcpResource an entry from an envelope.
func FromMcpResource(s *Schema, e *mcp.Resource) (Entry, error) {
	info, found := s.Lookup(e.Body.TypeUrl)
	if !found {
		return Entry{}, fmt.Errorf("resource Type not recognized: %v", e.Body.TypeUrl)
	}

	p := info.NewProtoInstance()
	if err := proto.Unmarshal(e.Body.Value, p); err != nil {
		return Entry{}, fmt.Errorf("error unmarshaling proto: %v", err)
	}

	return Entry{
		ID: VersionedKey{
			Version: Version(e.Metadata.Version),
			Key: Key{
				TypeURL:  info.TypeURL,
				FullName: FullName{e.Metadata.Name},
			},
		},
		Item: p,
	}, nil
}

// FromMcpResourceAll extracts all entries from the given envelopes and returns.
func FromMcpResourceAll(s *Schema, es []*mcp.Resource) ([]Entry, error) {
	result := make([]Entry, len(es))
	for i, e := range es {
		r, err := FromMcpResource(s, e)
		if err != nil {
			return nil, err
		}
		result[i] = r
	}
	return result, nil
}
