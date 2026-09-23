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

package options

import (
	"os"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

func TestDeltaNDSEnabled(t *testing.T) {
	old, set := os.LookupEnv("ISTIO_META_DELTA_NDS")
	if err := os.Unsetenv("ISTIO_META_DELTA_NDS"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ISTIO_METAJSON_DELTA_NDS_TEST", "{}")
	t.Cleanup(func() {
		if set {
			_ = os.Setenv("ISTIO_META_DELTA_NDS", old)
		} else {
			_ = os.Unsetenv("ISTIO_META_DELTA_NDS")
		}
	})

	for name, tc := range map[string]struct {
		metadata map[string]string
		want     bool
	}{
		"unset":    {},
		"enabled":  {metadata: map[string]string{"ISTIO_META_DELTA_NDS": "true"}, want: true},
		"disabled": {metadata: map[string]string{"ISTIO_META_DELTA_NDS": "false"}},
		"invalid":  {metadata: map[string]string{"ISTIO_META_DELTA_NDS": "enabled"}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := &meshconfig.ProxyConfig{ProxyMetadata: tc.metadata}
			if got := deltaNDSEnabled(cfg); got != tc.want {
				t.Fatalf("deltaNDSEnabled() = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("environment enables when metadata disables", func(t *testing.T) {
		t.Setenv("ISTIO_META_DELTA_NDS", "true")
		cfg := &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{"ISTIO_META_DELTA_NDS": "false"}}
		if !deltaNDSEnabled(cfg) {
			t.Fatal("explicit environment value did not override ProxyConfig")
		}
	})

	t.Run("environment disables when metadata enables", func(t *testing.T) {
		t.Setenv("ISTIO_META_DELTA_NDS", "false")
		cfg := &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{"ISTIO_META_DELTA_NDS": "true"}}
		if deltaNDSEnabled(cfg) {
			t.Fatal("explicit environment value did not override ProxyConfig")
		}
	})

	t.Run("JSON metadata enables when environment disables", func(t *testing.T) {
		t.Setenv("ISTIO_META_DELTA_NDS", "false")
		t.Setenv("ISTIO_METAJSON_DELTA_NDS_TEST", `{"DELTA_NDS":"true"}`)
		cfg := &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{"ISTIO_META_DELTA_NDS": "false"}}
		if !deltaNDSEnabled(cfg) {
			t.Fatal("JSON metadata did not override the plain environment value")
		}
	})

	t.Run("JSON metadata disables when environment enables", func(t *testing.T) {
		t.Setenv("ISTIO_META_DELTA_NDS", "true")
		t.Setenv("ISTIO_METAJSON_DELTA_NDS_TEST", `{"DELTA_NDS":"false"}`)
		cfg := &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{"ISTIO_META_DELTA_NDS": "true"}}
		if deltaNDSEnabled(cfg) {
			t.Fatal("JSON metadata did not override the plain environment value")
		}
	})
}
