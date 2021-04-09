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

package kube

import "testing"

func TestIsAtLeastVersion(t *testing.T) {
	tests := []struct {
		name           string
		clusterVersion uint
		minorVersion   uint
		want           bool
	}{
		{
			name:           "exact match",
			clusterVersion: 15,
			minorVersion:   15,
			want:           true,
		},
		{
			name:           "too old",
			clusterVersion: 14,
			minorVersion:   15,
			want:           false,
		},
		{
			name:           "newer",
			clusterVersion: 16,
			minorVersion:   15,
			want:           true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &MockClient{KubernetesVersion: tt.clusterVersion}
			if got := IsAtLeastVersion(cl, tt.minorVersion); got != tt.want {
				t.Errorf("IsAtLeastVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsLessThanVersionVersion(t *testing.T) {
	tests := []struct {
		name           string
		clusterVersion uint
		minorVersion   uint
		want           bool
	}{
		{
			name:           "exact match",
			clusterVersion: 15,
			minorVersion:   15,
			want:           false,
		},
		{
			name:           "older",
			clusterVersion: 14,
			minorVersion:   15,
			want:           true,
		},
		{
			name:           "too new",
			clusterVersion: 16,
			minorVersion:   15,
			want:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := &MockClient{KubernetesVersion: tt.clusterVersion}
			if got := IsLessThanVersion(cl, tt.minorVersion); got != tt.want {
				t.Errorf("IsLessThanVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}
