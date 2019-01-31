//  Copyright 2019 Istio Authors
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

package component

import (
	"testing"
)

type testRequirement struct {
	name string
}

func (t testRequirement) String() string {
	return t.name
}

type testConfiguration struct {
	content string
}

func (t testConfiguration) String() string {
	return t.content
}

func TestConfiguredRequirements(t *testing.T) {
	testReq := testRequirement{"testing"}
	testConfig := testConfiguration{"{spec: 'blah blah blah'}"}

	tests := []struct {
		desc string
		name string
		req  testRequirement
		conf *testConfiguration
	}{
		{
			desc: "name: '', config: nil",
			name: "",
			req:  testReq,
		},
		{
			desc: "name: 'alice', config: nil",
			name: "alice",
			req:  testReq,
		},
		{
			desc: "name: '', config: present",
			name: "",
			req:  testReq,
			conf: &testConfig,
		},
		{
			desc: "name: 'charlie', config: present",
			name: "charlie",
			req:  testReq,
			conf: &testConfig,
		},
	}

	for _, rt := range tests {
		t.Run(rt.desc, func(t *testing.T) {
			var req *ConfiguredRequirement = nil
			if rt.conf == nil {
				req = NewNamedRequirement(rt.name, rt.req)
			} else {
				req = NewConfiguredRequirement(rt.name, rt.req, *rt.conf)
			}
			if req.GetName() != rt.name {
				t.Fatal("expected requirement name '", rt.name, "' got '", req.GetName(), "'")
			}
			if req.GetRequirement() != rt.req {
				t.Fatal("expected requirement ", rt.req, " got ", req.GetRequirement())
			}
			if rt.conf != nil && req.GetConfiguration() != *rt.conf {
				t.Fatal("expected configuration ", rt.conf, " got ", req.GetConfiguration())
			}
		})
	}
}
