// Copyright 2019 Istio Authors
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
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mccpb "istio.io/api/mixer/v1/config/client"
	"istio.io/istio/pilot/pkg/model"
	context "istio.io/istio/pilot/pkg/model"
)

func TestTransportConfig(t *testing.T) {
	cases := []struct {
		mesh   meshconfig.MeshConfig
		node   model.Proxy
		expect *mccpb.NetworkFailPolicy
	}{
		{
			// defaults set
			mesh: context.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_CLOSE,
				MaxRetry:      defaultRetries,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
		{
			// retry and retry times set
			mesh: context.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{
					model.NodeMetadataPolicyCheckRetries:           "5",
					model.NodeMetadataPolicyCheckBaseRetryWaitTime: "1m",
					model.NodeMetadataPolicyCheckMaxRetryWaitTime:  "1.5s",
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_CLOSE,
				MaxRetry:      5,
				BaseRetryWait: types.DurationProto(1 * time.Minute),
				MaxRetryWait:  types.DurationProto(1500 * time.Millisecond),
			},
		},
		{
			// just retry amount set
			mesh: context.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{
					model.NodeMetadataPolicyCheckRetries: "1",
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_CLOSE,
				MaxRetry:      1,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
		{
			// fail open from node metadata
			mesh: context.DefaultMeshConfig(),
			node: model.Proxy{
				Metadata: map[string]string{
					model.NodeMetadataPolicyCheck: policyCheckDisable,
				},
			},
			expect: &mccpb.NetworkFailPolicy{
				Policy:        mccpb.FAIL_OPEN,
				MaxRetry:      defaultRetries,
				BaseRetryWait: defaultBaseRetryWaitTime,
				MaxRetryWait:  defaultMaxRetryWaitTime,
			},
		},
	}
	for _, c := range cases {
		tc := buildTransport(&c.mesh, &c.node)
		if !reflect.DeepEqual(tc.NetworkFailPolicy, c.expect) {
			t.Errorf("got %v, expected %v", tc.NetworkFailPolicy, c.expect)
		}
	}
}
