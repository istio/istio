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

	"github.com/bradfitz/slice"
	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	proto "github.com/gogo/protobuf/types"
)

// GetDynamicListenerDump retrieves a listener dump with just dynamic active listeners in it
func (w *Wrapper) GetDynamicListenerDump(stripVersions bool) (*adminapi.ListenersConfigDump, error) {
	listenerDump, err := w.GetListenerConfigDump()
	if err != nil {
		return nil, err
	}
	dal := listenerDump.GetDynamicActiveListeners()
	slice.Sort(dal, func(i, j int) bool {
		return dal[i].Listener.Name < dal[j].Listener.Name
	})
	if stripVersions {
		for i := range dal {
			dal[i].VersionInfo = ""
			dal[i].LastUpdated = nil
		}
	}
	return &adminapi.ListenersConfigDump{DynamicActiveListeners: dal}, nil
}

// GetListenerConfigDump retrieves the listener config dump from the ConfigDump
func (w *Wrapper) GetListenerConfigDump() (*adminapi.ListenersConfigDump, error) {
	if w.Configs == nil {
		return nil, fmt.Errorf("config dump has no listener dump")
	}
	listenerDumpAny, ok := w.Configs["listeners"]
	if !ok {
		return nil, fmt.Errorf("config dump has no listener dump")
	}
	listenerDump := &adminapi.ListenersConfigDump{}
	err := proto.UnmarshalAny(&listenerDumpAny, listenerDump)
	if err != nil {
		return nil, err
	}
	return listenerDump, nil
}
