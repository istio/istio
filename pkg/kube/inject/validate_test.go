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

package inject

import (
	"testing"
)

func TestValidateExcludeInterfaces(t *testing.T) {
	tests := []struct {
		name       string
		interfaces string
		wantErr    bool
	}{
		{
			name:       "empty string",
			interfaces: "",
			wantErr:    false,
		},
		{
			name:       "single valid interface",
			interfaces: "eth0",
			wantErr:    false,
		},
		{
			name:       "multiple valid interfaces",
			interfaces: "eth0,eth1,lo",
			wantErr:    false,
		},
		{
			name:       "interface with underscore",
			interfaces: "eth_0",
			wantErr:    false,
		},
		{
			name:       "interface with hyphen",
			interfaces: "eth-0",
			wantErr:    false,
		},
		{
			name:       "interface with dot",
			interfaces: "eth0.1",
			wantErr:    false,
		},
		{
			name:       "interface starting with number",
			interfaces: "0eth",
			wantErr:    false,
		},
		{
			name:       "interface starting with underscore",
			interfaces: "_eth0",
			wantErr:    false,
		},
		{
			name:       "complex valid names",
			interfaces: "veth123abc,br-docker0,bond0.100",
			wantErr:    false,
		},
		{
			name:       "max length interface (15 chars)",
			interfaces: "123456789012345",
			wantErr:    false,
		},
		{
			name:       "too long interface name (16 chars)",
			interfaces: "1234567890123456",
			wantErr:    true,
		},
		{
			name:       "interface with space - injection attempt",
			interfaces: "eth0 -d 10.0.0.50",
			wantErr:    true,
		},
		{
			name:       "iptables injection attempt",
			interfaces: "eth0 -d 10.0.0.50 -p tcp --dport 8080",
			wantErr:    true,
		},
		{
			name:       "interface with semicolon",
			interfaces: "eth0;echo",
			wantErr:    true,
		},
		{
			name:       "interface with pipe",
			interfaces: "eth0|cat",
			wantErr:    true,
		},
		{
			name:       "interface with backtick",
			interfaces: "eth0`id`",
			wantErr:    true,
		},
		{
			name:       "interface with dollar sign",
			interfaces: "eth0$PATH",
			wantErr:    true,
		},
		{
			name:       "empty interface in list",
			interfaces: "eth0,,eth1",
			wantErr:    true,
		},
		{
			name:       "interface with leading space",
			interfaces: " eth0",
			wantErr:    false, // trimmed during validation
		},
		{
			name:       "interface with trailing space",
			interfaces: "eth0 ",
			wantErr:    false, // trailing space is trimmed, resulting in valid "eth0"
		},
		{
			name:       "valid interface with spaces around comma",
			interfaces: "eth0 , eth1",
			wantErr:    false, // spaces around comma are trimmed
		},
		{
			name:       "interface with slash",
			interfaces: "eth0/1",
			wantErr:    true,
		},
		{
			name:       "interface with colon",
			interfaces: "eth0:1",
			wantErr:    true,
		},
		{
			name:       "interface with at sign",
			interfaces: "veth@if123",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExcludeInterfaces(tt.interfaces)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateExcludeInterfaces(%q) error = %v, wantErr %v", tt.interfaces, err, tt.wantErr)
			}
		})
	}
}
