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

package xds_test

import (
	"fmt"
	"testing"
	"time"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

// TestRegistration is an e2e test for registration. Most tests are in autoregister package, but this
// exercises the full XDS flow.
func TestRegistration(t *testing.T) {
	// TODO: allow fake XDS to be "authenticated"
	test.SetForTest(t, &features.ValidateWorkloadEntryIdentity, false)
	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	ds.Store().Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadGroup,
			Name:             "wg",
			Namespace:        "namespace",
		},
		Spec: &v1alpha3.WorkloadGroup{
			Template: &v1alpha3.WorkloadEntry{
				Labels: map[string]string{
					"merge": "wg",
					"wg":    "1",
				},
			},
		},
		Status: nil,
	})
	proxy := &model.Proxy{
		Labels:      map[string]string{"merge": "me"},
		IPAddresses: []string{"1.1.1.1"},
		Metadata: &model.NodeMetadata{
			AutoRegisterGroup: "wg",
			Namespace:         "namespace",
			Network:           "network1",
			Labels:            map[string]string{"merge": "meta", "meta": "2"},
		},
	}
	ds.Connect(ds.SetupProxy(proxy), nil, nil)
	var we *config.Config
	retry.UntilSuccessOrFail(t, func() error {
		we = ds.Store().Get(gvk.WorkloadEntry, "wg-1.1.1.1-network1", "namespace")
		if we == nil {
			return fmt.Errorf("no WE found")
		}
		return nil
	}, retry.Timeout(time.Second*10))
	assert.Equal(t, we.Spec.(*v1alpha3.WorkloadEntry).Labels, map[string]string{
		"merge": "meta",
		"meta":  "2",
		"wg":    "1",
	})
}
