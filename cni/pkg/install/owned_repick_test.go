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
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	ownedName   = "02-istio-cni.conflist"
	antreaName  = "05-antrea.conflist"
	primaryName = "10-primary.conflist"
)

func ownedTestConfig(t *testing.T) *config.InstallConfig {
	dir := t.TempDir()
	if err := file.AtomicCopy(filepath.Join("testdata", "list.conflist"), dir, primaryName); err != nil {
		t.Fatal(err)
	}
	cfg := &config.InstallConfig{
		MountedCNINetDir:            dir,
		ChainedCNIPlugin:            true,
		AmbientEnabled:              true,
		IstioOwnedCNIConfig:         true,
		IstioOwnedCNIConfigFilename: ownedName,
		PluginLogLevel:              "debug",
		CNIAgentRunDir:              kubeconfigFilename,
		PodNamespace:                "my-namespace",
		CNIBinTargetDirs:            []string{t.TempDir()},
	}
	isReady := &atomic.Value{}
	isReady.Store(false)
	NewInstaller(cfg, isReady)
	return cfg
}

// install mimics one installAll pass over the CNI config.
func install(t *testing.T, cfg *config.InstallConfig, path string) (string, error) {
	t.Helper()
	checkErr := checkValidCNIConfig(cfg, path)
	if checkErr == nil {
		return path, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	newPath, err := createCNIConfigFile(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return newPath, checkErr
}

func pluginTypes(t *testing.T, path string) []string {
	t.Helper()
	m, err := util.ReadCNIConfigMap(path)
	if err != nil {
		t.Fatal(err)
	}
	plugins, err := util.GetPlugins(m)
	if err != nil {
		t.Fatal(err)
	}
	return pluginTypesOf(plugins)
}

// antreaChain writes what Antrea's chaining installer writes: a copy of src plus antrea.
func antreaChain(t *testing.T, dir, src string) {
	t.Helper()
	m, err := util.ReadCNIConfigMap(filepath.Join(dir, src))
	if err != nil {
		t.Fatal(err)
	}
	plugins, err := util.GetPlugins(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(pluginTypesOf(plugins), ","), "antrea") {
		m["plugins"] = append(plugins, map[string]any{"type": "antrea"})
	}
	b, err := util.MarshalCNIConfig(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, antreaName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func pluginTypesOf(plugins []any) []string {
	var types []string
	for _, p := range plugins {
		types = append(types, p.(map[string]any)["type"].(string))
	}
	return types
}

func TestOwnedConfigFollowsCNIChainedInFront(t *testing.T) {
	cfg := ownedTestConfig(t)
	dir := cfg.MountedCNINetDir
	owned := filepath.Join(dir, ownedName)

	path, _ := install(t, cfg, "")
	path, err := install(t, cfg, path)
	assert.NoError(t, err)
	assert.Equal(t, cfg.CNIConfName, primaryName)

	// Antrea installs after istio-cni and chains on our config.
	antreaChain(t, dir, ownedName)

	path, err = install(t, cfg, path)
	if err == nil || !strings.Contains(err.Error(), "primary CNI config changed") {
		t.Fatalf("expected primary change, got %v", err)
	}
	assert.Equal(t, cfg.CNIConfName, antreaName)
	types := pluginTypes(t, owned)
	assert.Equal(t, types[len(types)-2:], []string{"antrea", "istio-cni"})
	assertNoIstioCNI(t, filepath.Join(dir, antreaName))

	// Antrea re-copies our config, putting istio-cni back in its copy. istio-cni must strip it
	// without rewriting the unchanged owned config: a rewrite makes Antrea copy again, forever.
	before, err := os.Stat(owned)
	if err != nil {
		t.Fatal(err)
	}
	antreaChain(t, dir, ownedName)
	path, _ = install(t, cfg, path)
	_, err = install(t, cfg, path)
	assert.NoError(t, err)
	after, err := os.Stat(owned)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatalf("unchanged %s was rewritten", ownedName)
	}
	types = pluginTypes(t, owned)
	assert.Equal(t, types[len(types)-2:], []string{"antrea", "istio-cni"})
	assertNoIstioCNI(t, filepath.Join(dir, antreaName))
}

func TestOwnedConfigConfiguredPrimaryStaysPinned(t *testing.T) {
	cfg := ownedTestConfig(t)
	cfg.CNIConfName = primaryName
	cfg.CNIConfNameDetected = false
	dir := cfg.MountedCNINetDir

	path, _ := install(t, cfg, "")
	antreaChain(t, dir, ownedName)

	_, err := install(t, cfg, path)
	assert.NoError(t, err)
	assert.Equal(t, cfg.CNIConfName, primaryName)
}

func TestCleanupOwnedStripsChainedCopies(t *testing.T) {
	cfg := ownedTestConfig(t)
	dir := cfg.MountedCNINetDir

	path, _ := install(t, cfg, "")
	antreaChain(t, dir, ownedName)

	isReady := &atomic.Value{}
	isReady.Store(false)
	installer := NewInstaller(cfg, isReady)
	installer.cniConfigFilepath = path
	if err := installer.Cleanup(); err != nil {
		t.Fatal(err)
	}

	if file.Exists(path) {
		t.Fatalf("%s was not removed", path)
	}
	assertNoIstioCNI(t, filepath.Join(dir, antreaName))
}

func assertNoIstioCNI(t *testing.T, path string) {
	t.Helper()
	for _, typ := range pluginTypes(t, path) {
		if typ == "istio-cni" {
			t.Fatalf("%s still lists istio-cni", path)
		}
	}
}
