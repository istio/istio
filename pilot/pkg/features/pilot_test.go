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

package features

import (
	"os"
	"testing"
	"time"
)

func Test_TerminationDrainDuration(t *testing.T) {
	tests := []struct {
		name      string
		setEnvVar bool
		envVar    string
		want      time.Duration
	}{
		{
			name:      "Returns 20 seconds when env var is set to 20",
			setEnvVar: true,
			envVar:    "20",
			want:      time.Second * 20,
		},
		{
			name:      "Returns 5 seconds when no env var set",
			setEnvVar: false,
			want:      time.Second * 5,
		},
		{
			name:      "Returns 5 seconds when env var is empty string",
			setEnvVar: true,
			envVar:    "",
			want:      time.Second * 5,
		},
		{
			name:      "Returns 5 seconds when env var is not an integer",
			setEnvVar: true,
			envVar:    "NaN",
			want:      time.Second * 5,
		},
		{
			name:      "Returns 20 seconds when env var is set to 20",
			setEnvVar: true,
			envVar:    "20",
			want:      time.Second * 20,
		},
		{
			name:      "Returns 0 seconds when env var is set to 0",
			setEnvVar: true,
			envVar:    "0",
			want:      time.Second * 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnvVar {
				os.Setenv(terminationDrainDurationVar.Name, tt.envVar)
			} else {
				os.Unsetenv(terminationDrainDurationVar.Name)
			}
			if got := TerminationDrainDuration(); got != tt.want {
				t.Errorf("TerminationDrainDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_KubernetesAPIQPS(t *testing.T) {
	tests := []struct {
		name      string
		setEnvVar bool
		envVar    string
		want      float64
	}{
		{
			name:      "Returns 100 when env var is set to 100",
			setEnvVar: true,
			envVar:    "100",
			want:      100.0,
		},
		{
			name:      "Returns 20.0 when when no env var set",
			setEnvVar: false,
			want:      20.0,
		},
		{
			name:      "Returns 20.0 when env var is empty string",
			setEnvVar: true,
			envVar:    "",
			want:      20.0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnvVar {
				os.Setenv(KubernetesAPIQPS.Name, tt.envVar)
			} else {
				os.Unsetenv(KubernetesAPIQPS.Name)
			}
			if got := KubernetesAPIQPS.Get(); got != tt.want {
				t.Errorf("KubernetesAPIQPS.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_KubernetesAPIBurst(t *testing.T) {
	tests := []struct {
		name      string
		setEnvVar bool
		envVar    string
		want      int
	}{
		{
			name:      "Returns 100 when env var is set to 100",
			setEnvVar: true,
			envVar:    "100",
			want:      100,
		},
		{
			name:      "Returns 40 when when no env var set",
			setEnvVar: false,
			want:      40,
		},
		{
			name:      "Returns 40 when env var is empty string",
			setEnvVar: true,
			envVar:    "",
			want:      40,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnvVar {
				os.Setenv(KubernetesAPIBurst.Name, tt.envVar)
			} else {
				os.Unsetenv(KubernetesAPIBurst.Name)
			}
			if got := KubernetesAPIBurst.Get(); got != tt.want {
				t.Errorf("KubernetesAPIBurst.Get() = %v, want %v", got, tt.want)
			}
		})
	}
}
