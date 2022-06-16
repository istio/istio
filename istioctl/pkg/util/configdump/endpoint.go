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
	"fmt"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
)

// GetEndpointsConfigDump retrieves the listener config dump from the ConfigDump
func (w *Wrapper) GetEndpointsConfigDump() (*adminapi.EndpointsConfigDump, error) {
	endpointsDumpAny, err := w.getSection(endpoints)
	if err != nil {
		return nil, fmt.Errorf("endpoints not found (was include_eds=true used?): %v", err)
	}
	endpointsDump := &adminapi.EndpointsConfigDump{}
	err = endpointsDumpAny.UnmarshalTo(endpointsDump)
	if err != nil {
		return nil, err
	}
	return endpointsDump, nil
}
