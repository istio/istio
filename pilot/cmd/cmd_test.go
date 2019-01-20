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

package cmd

import (
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestValidateMeshConfig(t *testing.T) {
	cases := []struct {
		name  string
		in    meshconfig.MeshConfig
		valid bool
	}{
		{
			name:  "valid mesh config without LocalityLoadBalancerSetting",
			in:    meshconfig.MeshConfig{},
			valid: true,
		},

		{
			name: "invalid mesh config with LocalityLoadBalancerSetting_Distribute total weight > 100",
			in: meshconfig.MeshConfig{
				LocalityLbSetting: &meshconfig.LocalityLoadBalancerSetting{
					Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
						{
							From: "a/b/c",
							To: map[string]uint32{
								"a/b/c": 80,
								"a/b1":  25,
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid mesh config with LocalityLoadBalancerSetting_Distribute total weight < 100",
			in: meshconfig.MeshConfig{
				LocalityLbSetting: &meshconfig.LocalityLoadBalancerSetting{
					Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
						{
							From: "a/b/c",
							To: map[string]uint32{
								"a/b/c": 80,
								"a/b1":  15,
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid mesh config with LocalityLoadBalancerSetting_Distribute weight = 0",
			in: meshconfig.MeshConfig{
				LocalityLbSetting: &meshconfig.LocalityLoadBalancerSetting{
					Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
						{
							From: "a/b/c",
							To: map[string]uint32{
								"a/b/c": 0,
								"a/b1":  100,
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid mesh config with LocalityLoadBalancerSetting specify both distribute and failover",
			in: meshconfig.MeshConfig{
				LocalityLbSetting: &meshconfig.LocalityLoadBalancerSetting{
					Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
						{
							From: "a/b/c",
							To: map[string]uint32{
								"a/b/c": 80,
								"a/b1":  20,
							},
						},
					},
					Failover: []*meshconfig.LocalityLoadBalancerSetting_Failover{
						{
							From: "region1",
							To:   "region2",
						},
					},
				},
			},
			valid: false,
		},

		{
			name: "invalid mesh config with failover src and dst have same region",
			in: meshconfig.MeshConfig{
				LocalityLbSetting: &meshconfig.LocalityLoadBalancerSetting{

					Failover: []*meshconfig.LocalityLoadBalancerSetting_Failover{
						{
							From: "region1",
							To:   "region1",
						},
					},
				},
			},
			valid: false,
		},
	}

	for _, c := range cases {
		if got := ValidateMeshConfig(&c.in); (got == nil) != c.valid {
			t.Errorf("ValidateMeshConfig failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateLocalities(t *testing.T) {
	cases := []struct {
		name       string
		localities []string
		valid      bool
	}{
		{
			name:       "multi wildcard locality",
			localities: []string{"*/zone/*"},
			valid:      false,
		},
		{
			name:       "wildcard not in suffix",
			localities: []string{"*/zone"},
			valid:      false,
		},
		{
			name:       "explicit wildcard region overlap",
			localities: []string{"*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard region overlap",
			localities: []string{"a", "a/b/c"},
			valid:      false,
		},
		{
			name:       "explicit wildcard zone overlap",
			localities: []string{"a/*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard zone overlap",
			localities: []string{"a/b", "a/b/c"},
			valid:      false,
		},
		{
			name:       "explicit wildcard subzone overlap",
			localities: []string{"a/b/*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard subzone overlap",
			localities: []string{"a/b", "a/b/c"},
			valid:      false,
		},
		{
			name:       "valid localities",
			localities: []string{"a1/*", "a2/*", "a3/b3/c3", "a4/b4", "a5/b5/*"},
			valid:      true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateLocalities(c.localities)
			if !c.valid && err == nil {
				t.Errorf("expect invalid localities")
			}

			if c.valid && err != nil {
				t.Errorf("expect valid localities. but got err %v", err)
			}
		})
	}

}
