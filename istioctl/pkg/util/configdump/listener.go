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
	"sort"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/golang/protobuf/ptypes"

	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

// GetDynamicListenerDump retrieves a listener dump with just dynamic active listeners in it
func (w *Wrapper) GetDynamicListenerDump(stripVersions bool) (*adminapi.ListenersConfigDump, error) {
	listenerDump, err := w.GetListenerConfigDump()
	if err != nil {
		return nil, err
	}

	dal := make([]*adminapi.ListenersConfigDump_DynamicListener, 0)
	for _, l := range listenerDump.DynamicListeners {
		// If a listener was reloaded, it would contain both the active and draining state
		// delete the draining state for proper comparison
		l.DrainingState = nil
		if l.ActiveState != nil {
			dal = append(dal, l)
		}
	}

	sort.Slice(dal, func(i, j int) bool {
		l := &listener.Listener{}
		// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
		dal[i].ActiveState.Listener.TypeUrl = v3.ListenerType
		dal[j].ActiveState.Listener.TypeUrl = v3.ListenerType
		err = ptypes.UnmarshalAny(dal[i].ActiveState.Listener, l)
		if err != nil {
			return false
		}
		name := l.Name
		err = ptypes.UnmarshalAny(dal[j].ActiveState.Listener, l)
		if err != nil {
			return false
		}
		return name < l.Name
	})
	if stripVersions {
		for i := range dal {
			dal[i].ActiveState.VersionInfo = ""
			dal[i].ActiveState.LastUpdated = nil
			dal[i].Name = "" // In Istio 1.5, Envoy creates this; suppress it
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
	err = ptypes.UnmarshalAny(listenerDumpAny, listenerDump)
	if err != nil {
		return nil, err
	}
	return listenerDump, nil
}
