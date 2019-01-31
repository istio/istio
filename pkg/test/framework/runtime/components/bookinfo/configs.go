//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package bookinfo

import (
	"path"
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
)

// ConfigFile represents config yaml files for different bookinfo scenarios.
type ConfigFile string

const (
	// NetworkingBookinfoGateway uses "networking/bookinfo-gateway.yaml"
	NetworkingBookinfoGateway ConfigFile = "networking/bookinfo-gateway.yaml"

	// NetworkingDestinationRuleAll uses "networking/destination-rule-all.yaml"
	NetworkingDestinationRuleAll ConfigFile = "networking/destination-rule-all.yaml"

	// NetworkingDestinationRuleAllMtls uses "networking/destination-rule-all-mtls.yaml"
	NetworkingDestinationRuleAllMtls ConfigFile = "networking/destination-rule-all-mtls.yaml"

	// NetworkingVirtualServiceAllV1 uses "networking/virtual-service-all-v1.yaml"
	NetworkingVirtualServiceAllV1 ConfigFile = "networking/virtual-service-all-v1.yaml"

	// NetworkingTcpDbRule uses "networking/virtual-service-ratings-db.yaml"
	NetworkingTCPDbRule ConfigFile = "networking/virtual-service-ratings-db.yaml"

	// MixerRuleRatingsRatelimit uses "policy/mixer-rule-ratings-ratelimit.yaml"
	MixerRuleRatingsRatelimit ConfigFile = "policy/mixer-rule-ratings-ratelimit.yaml"

	// MixerRuleRatingsDenial uses "policy/mixer-rule-ratings-denial.yaml"
	MixerRuleRatingsDenial ConfigFile = "policy/mixer-rule-ratings-denial.yaml"

	// MixerRuleIngressDenial uses "policy/mixer-rule-ingress-denial.yaml"
	MixerRuleIngressDenial ConfigFile = "policy/mixer-rule-ingress-denial.yaml"
)

// LoadOrFail loads a Book Info configuration file from the system and returns its contents.
func (l ConfigFile) LoadOrFail(t testing.TB) string {
	t.Helper()
	p := path.Join(env.BookInfoRoot, string(l))

	content, err := test.ReadConfigFile(p)
	if err != nil {
		t.Fatalf("unable to load config %s at %v, err:%v", l, p, err)
	}

	return content
}

func GetDestinationRuleConfigFile(t testing.TB, ctx component.Repository) ConfigFile {
	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		t.Fatalf("Could not get test environment: %v", err)
	}
	if env.IsMtlsEnabled() {
		return NetworkingDestinationRuleAllMtls
	}
	return NetworkingDestinationRuleAll
}
