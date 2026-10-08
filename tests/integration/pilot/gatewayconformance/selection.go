//go:build integ

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

// Package gatewayconformance holds helpers shared by the Gateway API conformance
// runners under tests/integration/pilot.
package gatewayconformance

import (
	"fmt"

	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"

	"istio.io/istio/pkg/util/sets"
)

// SkipNonStandardTests returns skipTests extended with every conformance test that requires a
// Gateway API feature outside the standard channel.
func SkipNonStandardTests(conformanceTests []suite.ConformanceTest, skippedTests map[string]string) map[string]string {
	skips := make(map[string]string, len(skippedTests))
	for name, reason := range skippedTests {
		skips[name] = reason
	}

	standardFeatures := sets.New[features.FeatureName]()
	for f := range features.AllFeatures {
		if f.Channel == features.FeatureChannelStandard {
			standardFeatures.Insert(f.Name)
		}
	}

	for _, test := range conformanceTests {
		if _, exists := skips[test.ShortName]; exists {
			continue
		}
		for _, name := range test.Features {
			if !standardFeatures.Contains(name) {
				skips[test.ShortName] = fmt.Sprintf("requires non-standard Gateway API feature %q", name)
				break
			}
		}
	}
	return skips
}
