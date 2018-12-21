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

package envelope

import (
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// Envelope converts a resource entry into its enveloped form.
func Envelope(e resource.Entry) (*mcp.Envelope, error) {

	serialized, err := proto.Marshal(e.Item)
	if err != nil {
		scope.Errorf("Error serializing proto from source e: %v:", e)
		return nil, err
	}

	createTime, err := types.TimestampProto(e.ID.CreateTime)
	if err != nil {
		scope.Errorf("Error parsing resource create_time for event (%v): %v", e, err)
		return nil, err
	}

	entry := &mcp.Envelope{
		Metadata: &mcp.Metadata{
			Name:       e.ID.FullName.String(),
			CreateTime: createTime,
			Version:    string(e.ID.Version),
		},
		Resource: &types.Any{
			TypeUrl: e.ID.TypeURL.String(),
			Value:   serialized,
		},
	}

	return entry, nil
}
