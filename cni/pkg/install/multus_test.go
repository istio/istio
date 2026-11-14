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

package install

import (
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
)

func TestIstioOwnedConfigSkipsMultusWrapper(t *testing.T) {
	for _, tc := range []struct {
		name        string
		ownedExists bool
	}{
		{name: "select primary on initial install"},
		{name: "remain ready after restart", ownedExists: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "00-multus.conf"), []byte(`{"cniVersion":"0.3.1","name":"multus","type":"multus-shim"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := file.AtomicCopy(filepath.Join("testdata", "list-no-istio.conflist"), dir, "05-primary.conflist"); err != nil {
				t.Fatal(err)
			}
			ownedPath := filepath.Join(dir, "02-istio-conf.conflist")
			if tc.ownedExists {
				if err := file.AtomicCopy(filepath.Join("testdata", "istio-owned.conflist.golden"), dir, filepath.Base(ownedPath)); err != nil {
					t.Fatal(err)
				}
			}

			cfg := &config.InstallConfig{
				MountedCNINetDir:            dir,
				ChainedCNIPlugin:            true,
				AmbientEnabled:              true,
				IstioOwnedCNIConfig:         true,
				IstioOwnedCNIConfigFilename: filepath.Base(ownedPath),
				PluginLogLevel:              "debug",
				CNIAgentRunDir:              "/path/to/kubeconfig",
				PodNamespace:                "my-namespace",
			}
			path := ""
			if tc.ownedExists {
				path = ownedPath
			}
			err := checkValidCNIConfig(cfg, path)
			if tc.ownedExists && err != nil {
				t.Fatalf("valid owned config rejected: %v", err)
			}
			if !tc.ownedExists && err == nil {
				t.Fatal("initial installation must request a config write")
			}
			if cfg.CNIConfName != "05-primary.conflist" {
				t.Fatalf("primary config = %q, want 05-primary.conflist", cfg.CNIConfName)
			}
			if !tc.ownedExists {
				written, err := createCNIConfigFile(t.Context(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				if written != ownedPath {
					t.Fatalf("written config = %q, want %q", written, ownedPath)
				}
				conf, err := util.ReadCNIConfigMap(written)
				if err != nil {
					t.Fatal(err)
				}
				plugins, err := util.GetPlugins(conf)
				if err != nil {
					t.Fatal(err)
				}
				if len(plugins) == 0 {
					t.Fatal("owned CNI chain is empty")
				}
				primary, ok := plugins[0].(map[string]any)
				if !ok || primary["type"] != "bridge" {
					t.Fatalf("owned chain does not start with the primary CNI: %v", plugins)
				}
				if err := checkValidCNIConfig(cfg, written); err != nil {
					t.Fatalf("written owned config rejected: %v", err)
				}
			}
		})
	}
}

func TestIstioOwnedConfigRejectsExplicitMultusWrapper(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "00-multus.conf"), []byte(`{"cniVersion":"0.3.1","name":"multus","type":"multus-shim"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.InstallConfig{
		MountedCNINetDir:            dir,
		CNIConfName:                 "00-multus.conf",
		ChainedCNIPlugin:            true,
		AmbientEnabled:              true,
		IstioOwnedCNIConfig:         true,
		IstioOwnedCNIConfigFilename: "02-istio-conf.conflist",
	}
	if _, err := createCNIConfigFile(t.Context(), cfg); err == nil {
		t.Fatal("Multus wrapper must not be copied into Istio's owned CNI chain")
	}
	if _, err := os.Stat(filepath.Join(dir, cfg.IstioOwnedCNIConfigFilename)); !os.IsNotExist(err) {
		t.Fatalf("owned config was written from Multus wrapper: %v", err)
	}
}
