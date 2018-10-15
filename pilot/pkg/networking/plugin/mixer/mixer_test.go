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

package mixer

import (
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
)

func TestDisablePolicyChecks(t *testing.T) {
	disablePolicyChecks := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.Router,
		},
		Env: &model.Environment{
			Mesh: &meshconfig.MeshConfig{
				DisablePolicyChecks: true,
			},
		},
	}

	enablePolicyChecks := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.Router,
		},
		Env: &model.Environment{
			Mesh: &meshconfig.MeshConfig{
				DisablePolicyChecks: false,
			},
		},
	}

	disableClientPolicyChecksParams := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.Sidecar,
		},
		Env: &model.Environment{
			Mesh: &meshconfig.MeshConfig{
				EnableClientSidePolicyCheck: false,
			},
		},
	}

	enableClientPolicyChecks := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Node: &model.Proxy{
			Type: model.Sidecar,
		},
		Env: &model.Environment{
			Mesh: &meshconfig.MeshConfig{
				EnableClientSidePolicyCheck: true,
			},
		},
	}

	testCases := []struct {
		name        string
		inputParams *plugin.InputParams
		result      bool
	}{
		{
			name:        "disable policy checks",
			inputParams: disablePolicyChecks,
			result:      true,
		},
		{
			name:        "enable policy checks",
			inputParams: enablePolicyChecks,
			result:      false,
		},
		{
			name:        "disable client policy checks",
			inputParams: disableClientPolicyChecksParams,
			result:      true,
		},
		{
			name:        "enable client policy checks",
			inputParams: enableClientPolicyChecks,
			result:      false,
		},
	}

	for _, tc := range testCases {
		ret := disableClientPolicyChecks(tc.inputParams.Env.Mesh, tc.inputParams.Node)

		if tc.result != ret {
			t.Errorf("%s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}

func TestOnInboundListener(t *testing.T) {
	mixerCheckServerNotPresent := &plugin.InputParams{
		ListenerProtocol: plugin.ListenerProtocolHTTP,
		Env: &model.Environment{
			Mesh: &meshconfig.MeshConfig{
				MixerCheckServer:  "",
				MixerReportServer: "",
			},
		},
	}
	testCases := []struct {
		name          string
		inputParams   *plugin.InputParams
		mutableParams *plugin.MutableObjects
		result        error
	}{
		{
			name:        "mixer check and report server not available",
			inputParams: mixerCheckServerNotPresent,
			result:      nil,
		},
	}

	for _, tc := range testCases {
		p := NewPlugin()
		ret := p.OnInboundListener(tc.inputParams, tc.mutableParams)

		if tc.result != ret {
			t.Errorf("%s: expecting %v but got %v", tc.name, tc.result, ret)
		}
	}
}
