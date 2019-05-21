package main

import (
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"strings"
	"testing"
)

func TestValidateOptions(t *testing.T) {
	cases := []struct{
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
				workloadSdsCacheOptions.InitialBackoff = 9
			},
			errorMsg: "initial backoff should be within range 10 to 120000",
		},
		{
			name: "initial backoff too large",
			setExtraOptions: func() {
				workloadSdsCacheOptions.InitialBackoff = 120001
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
			InitialBackoff: 10,
		}
		serverOptions = sds.Options{
			EnableIngressGatewaySDS: true,
			EnableWorkloadSDS: true,
			IngressGatewayUDSPath: "/abc",
			WorkloadUDSPath: "/xyz",
			CAEndpoint: "endpoint",
			CAProviderName: "provider",
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
