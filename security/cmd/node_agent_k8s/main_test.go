// Copyright 2019 Istio Authors. All Rights Reserved.
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

package main

import (
	"strings"
	"testing"

	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
)

func TestValidateOptions(t *testing.T) {
	cases := []struct {
		name            string
		setExtraOptions func()
		errorMsg        string
	}{
		{
			name: "valid options",
		},
		{
			name: "initial backoff too small",
			setExtraOptions: func() {
				workloadSdsCacheOptions.InitialBackoffInMilliSec = 9
			},
			errorMsg: "initial backoff should be within range 10 to 120000",
		},
		{
			name: "initial backoff too large",
			setExtraOptions: func() {
				workloadSdsCacheOptions.InitialBackoffInMilliSec = 120001
			},
			errorMsg: "initial backoff should be within range 10 to 120000",
		},
		{
			name: "same path for workload SDS and ingress SDS",
			setExtraOptions: func() {
				serverOptions.IngressGatewayUDSPath = "/same"
				serverOptions.WorkloadUDSPath = "/same"
			},
			errorMsg: "UDS paths for ingress gateway and workload cannot be the same",
		},
		{
			name: "empty CA provider when workload SDS enabled",
			setExtraOptions: func() {
				serverOptions.CAProviderName = ""
			},
			errorMsg: "CA provider cannot be empty when workload SDS is enabled",
		},
		{
			name: "empty CA endpoint when workload SDS enabled",
			setExtraOptions: func() {
				serverOptions.CAEndpoint = ""
			},
			errorMsg: "CA endpoint cannot be empty when workload SDS is enabled",
		},
	}

	for _, c := range cases {

		// Set the valid options as the base for the testing.
		workloadSdsCacheOptions = cache.Options{
			InitialBackoffInMilliSec: 2000,
		}
		serverOptions = sds.Options{
			EnableIngressGatewaySDS: true,
			EnableWorkloadSDS:       true,
			IngressGatewayUDSPath:   "/abc",
			WorkloadUDSPath:         "/xyz",
			CAEndpoint:              "endpoint",
			CAProviderName:          "provider",
		}

		// Set extra options from each test case
		if c.setExtraOptions != nil {
			c.setExtraOptions()
		}

		got := validateOptions()
		if c.errorMsg == "" {
			if got != nil {
				t.Errorf("got %q but expect no error", got)
			}
		} else {
			if !strings.HasPrefix(got.Error(), c.errorMsg) {
				t.Errorf("got %q but expect to have error: %q", got, c.errorMsg)
			}
		}
	}
}
