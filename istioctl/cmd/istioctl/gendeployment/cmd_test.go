// Copyright 2017 Istio Authors
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

package gendeployment

import (
	"reflect"
	"strings"
	"testing"
)

func TestSetFeatures(t *testing.T) {
	tests := []struct {
		name     string
		features []string
		want     *installation
		err      string
	}{
		{"empty", []string{}, &installation{}, ""},
		{"routing", []string{"routing"}, &installation{Pilot: true}, ""},
		{"mtls", []string{"mtls"}, &installation{CA: true, Pilot: true}, ""},
		{"ingress", []string{"ingress"}, &installation{Ingress: true, Pilot: true}, ""},
		{"sidecar-injector", []string{"sidecar-injector"}, &installation{SidecarInjector: true}, ""},
		{"policy", []string{"policy"}, &installation{Mixer: true, Pilot: true}, ""},
		{"telemetry", []string{"telemetry"}, &installation{Mixer: true, Pilot: true}, ""},
		{"mtls + telemetry", []string{"mtls", "telemetry"}, &installation{Mixer: true, Pilot: true, CA: true}, ""},
		{"mtls + policy + sidecar-injector", []string{"mtls", "policy", "sidecar-injector"},
			&installation{Mixer: true, Pilot: true, CA: true, SidecarInjector: true}, ""},
		{"invalid", []string{"everything"}, nil, "everything"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := &installation{}
			err := actual.setFeatures(tt.features)
			if err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("installation{}.setFeatures(%v) = '%s', wanted no err", tt.features, err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			} else if !reflect.DeepEqual(actual, tt.want) {
				t.Fatalf("&installation{}.setFeatures(%v) = %#v, want %#v", tt.features, actual, tt.want)
			}
		})
	}
}
