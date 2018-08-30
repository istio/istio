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

package configdump

import (
	"fmt"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	proto "github.com/gogo/protobuf/types"
)

// GetBootstrapConfigDump retrieves the bootstrap config dump from the ConfigDump
func (w *Wrapper) GetBootstrapConfigDump() (*adminapi.BootstrapConfigDump, error) {
	// The bootstrap dump is the first one in the list.
	// See https://www.envoyproxy.io/docs/envoy/latest/api-v2/admin/v2alpha/config_dump.proto
	if len(w.Configs) < 1 {
		return nil, fmt.Errorf("config dump has no bootstrap dump")
	}
	bootstrapDumpAny := w.Configs[0]
	bootstrapDump := &adminapi.BootstrapConfigDump{}
	err := proto.UnmarshalAny(&bootstrapDumpAny, bootstrapDump)
	if err != nil {
		return nil, err
	}
	return bootstrapDump, nil
}
