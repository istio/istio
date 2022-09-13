/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package config

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	mcp "istio.io/api/mcp/v1alpha1"
)

// Convert from model.Config, which has no associated proto, to MCP Resource proto.
// TODO: define a proto matching Config - to avoid useless superficial conversions.
func PilotConfigToResource(c *Config) (*mcp.Resource, error) {
	r := &mcp.Resource{}

	// MCP, K8S and Istio configs use gogo configs
	// On the wire it's the same as golang proto.
	a, err := ToProto(c.Spec)
	if err != nil {
		return nil, err
	}
	r.Body = a
	r.Metadata = &mcp.Metadata{
		Name:        c.Namespace + "/" + c.Name,
		CreateTime:  timestamppb.New(c.CreationTimestamp),
		Version:     c.ResourceVersion,
		Labels:      c.Labels,
		Annotations: c.Annotations,
	}

	return r, nil
}
