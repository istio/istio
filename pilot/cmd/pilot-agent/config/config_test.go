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

package config

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
)

func TestGetMeshConfig(t *testing.T) {
	meshOverride := `
defaultConfig:
  discoveryAddress: foo:123
  controlPlaneAuthPolicy: NONE
  proxyMetadata:
    SOME: setting
  drainDuration: 1s`
	proxyOverride := `discoveryAddress: foo:123
proxyMetadata:
  SOME: setting
drainDuration: 1s
controlPlaneAuthPolicy: NONE`
	overridesExpected := func() *meshconfig.ProxyConfig {
		m := mesh.DefaultProxyConfig()
		m.DiscoveryAddress = "foo:123"
		m.ProxyMetadata = map[string]string{"SOME": "setting"}
		m.DrainDuration = durationpb.New(time.Second)
		m.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE
		return m
	}()
	cases := []struct {
		name        string
		annotation  string
		environment string
		file        string
		expect      *meshconfig.ProxyConfig
	}{
		{
			name: "Defaults",
			expect: func() *meshconfig.ProxyConfig {
				m := mesh.DefaultProxyConfig()
				return m
			}(),
		},
		{
			name:       "Annotation Override",
			annotation: proxyOverride,
			expect:     overridesExpected,
		},
		{
			name:   "File Override",
			file:   meshOverride,
			expect: overridesExpected,
		},
		{
			name:        "Environment Override",
			environment: proxyOverride,
			expect:      overridesExpected,
		},
		{
			// Hopefully no one actually has all three of these set in a real system, but we will still
			// test them all together.
			name: "Multiple Override",
			// Order is file < env < annotation
			file: `
defaultConfig:
  discoveryAddress: file:123
  proxyMetadata:
    SOME: setting
  drainDuration: 1s
  extraStatTags: ["a"]
  proxyStatsMatcher:
    inclusionPrefixes: ["a"]
    inclusionSuffixes: ["b"]
    inclusionRegexps: ["c"]
  controlPlaneAuthPolicy: NONE`,
			environment: `
discoveryAddress: environment:123
proxyMetadata:
OTHER: option`,
			annotation: `
discoveryAddress: annotation:123
proxyMetadata:
  ANNOTATION: something
drainDuration: 5s
extraStatTags: ["b"]
proxyStatsMatcher:
  inclusionPrefixes: ["a"]
  inclusionSuffixes: ["e"]
  inclusionRegexps: ["f"]
`,
			expect: func() *meshconfig.ProxyConfig {
				m := mesh.DefaultProxyConfig()
				m.DiscoveryAddress = "annotation:123"
				m.ProxyMetadata = map[string]string{"ANNOTATION": "something", "SOME": "setting"}
				m.DrainDuration = durationpb.New(5 * time.Second)
				m.ExtraStatTags = []string{"b"}
				m.ProxyStatsMatcher = &meshconfig.ProxyConfig_ProxyStatsMatcher{}
				m.ProxyStatsMatcher.InclusionPrefixes = []string{"a"}
				m.ProxyStatsMatcher.InclusionSuffixes = []string{"e"}
				m.ProxyStatsMatcher.InclusionRegexps = []string{"f"}
				m.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE
				return m
			}(),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxyConfigEnv := tt.environment
			got, err := getMeshConfig(tt.file, tt.annotation, proxyConfigEnv, true)
			if err != nil {
				t.Fatal(err)
			}
			if !cmp.Equal(got.DefaultConfig, tt.expect, protocmp.Transform()) {
				t.Fatalf("got \n%v expected \n%v", got.DefaultConfig, tt.expect)
			}
		})
	}
}
