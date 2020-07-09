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

package configdump

import (
	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/ptypes"
)

// GetEndpointConfigDump retrieves the endpoint config dump from the ConfigDump
func (w *Wrapper) GetEndpointConfigDump() (*adminapi.EndpointsConfigDump, error) {
	endpointDumpAny, err := w.getSection(endpoints)
	if err != nil {
		return nil, err
	}
	endpointDump := &adminapi.EndpointsConfigDump{}
	err = ptypes.UnmarshalAny(endpointDumpAny, endpointDump)
	if err != nil {
		return nil, err
	}
	return endpointDump, nil
}
