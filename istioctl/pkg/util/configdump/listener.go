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
	"sort"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/golang/protobuf/ptypes"
)

// GetDynamicListenerDump retrieves a listener dump with just dynamic active listeners in it
func (w *Wrapper) GetDynamicListenerDump(stripVersions bool) (*adminapi.ListenersConfigDump, error) {
	listenerDump, err := w.GetListenerConfigDump()
	if err != nil {
		return nil, err
	}

	dal := make([]*adminapi.ListenersConfigDump_DynamicListener, 0)
	for _, l := range listenerDump.DynamicListeners {
		if l.ActiveState != nil {
			dal = append(dal, l)
		}
	}

	sort.Slice(dal, func(i, j int) bool {
		listener := &xdsapi.Listener{}
		err = ptypes.UnmarshalAny(dal[i].ActiveState.Listener, listener)
		if err != nil {
			return false
		}
		name := listener.Name
		err = ptypes.UnmarshalAny(dal[j].ActiveState.Listener, listener)
		if err != nil {
			return false
		}
		return name < listener.Name
	})
	if stripVersions {
		for i := range dal {
			dal[i].ActiveState.VersionInfo = ""
			dal[i].ActiveState.LastUpdated = nil
		}
	}
	return &adminapi.ListenersConfigDump{DynamicListeners: dal}, nil
}

// GetListenerConfigDump retrieves the listener config dump from the ConfigDump
func (w *Wrapper) GetListenerConfigDump() (*adminapi.ListenersConfigDump, error) {
	listenerDumpAny, err := w.getSection(listeners)
	if err != nil {
		return nil, err
	}
	listenerDump := &adminapi.ListenersConfigDump{}
	err = ptypes.UnmarshalAny(&listenerDumpAny, listenerDump)
	if err != nil {
		return nil, err
	}
	return listenerDump, nil
}
