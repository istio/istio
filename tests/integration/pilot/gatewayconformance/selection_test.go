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

package gatewayconformance

import (
	"testing"

	"sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func TestSkipNonStandardTests(t *testing.T) {
	cases := []struct {
		name        string
		features    []features.FeatureName
		skipTests   map[string]string
		wantSkipped bool
		wantReason  string
	}{
		{
			name:     "standard features are kept",
			features: []features.FeatureName{features.SupportGateway, features.SupportTLSRouteModeTerminate},
		},
		{
			name: "no required features",
		},
		{
			name:        "experimental feature is skipped",
			features:    []features.FeatureName{features.SupportTLSRouteModeMixed},
			wantSkipped: true,
			wantReason:  `requires non-standard Gateway API feature "TLSRouteModeMixed"`,
		},
		{
			name:        "experimental feature alongside standard ones is skipped",
			features:    []features.FeatureName{features.SupportGateway, features.SupportHTTPRouteRetry},
			wantSkipped: true,
			wantReason:  `requires non-standard Gateway API feature "HTTPRouteRetry"`,
		},
		{
			name:        "unknown feature is treated as non-standard",
			features:    []features.FeatureName{"NotAFeature"},
			wantSkipped: true,
			wantReason:  `requires non-standard Gateway API feature "NotAFeature"`,
		},
		{
			name:        "existing reason is preserved",
			features:    []features.FeatureName{features.SupportTLSRouteModeMixed},
			skipTests:   map[string]string{"Case": "already excluded"},
			wantSkipped: true,
			wantReason:  "already excluded",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			conformanceTests := []suite.ConformanceTest{{ShortName: "Case", Features: tt.features}}
			skips := SkipNonStandardTests(conformanceTests, tt.skipTests)

			reason, skipped := skips["Case"]
			if skipped != tt.wantSkipped {
				t.Fatalf("Case skipped = %t, want %t", skipped, tt.wantSkipped)
			}
			if skipped && reason != tt.wantReason {
				t.Fatalf("Case reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestSkipNonStandardTestsNilInput(t *testing.T) {
	conformanceTests := []suite.ConformanceTest{
		{ShortName: "Experimental", Features: []features.FeatureName{features.SupportTLSRouteModeMixed}},
	}

	skips := SkipNonStandardTests(conformanceTests, nil)
	if _, skipped := skips["Experimental"]; !skipped {
		t.Fatalf("Experimental not skipped: %v", skips)
	}
	if len(skips) != 1 {
		t.Fatalf("skips = %v, want exactly 1 entry", skips)
	}
}
