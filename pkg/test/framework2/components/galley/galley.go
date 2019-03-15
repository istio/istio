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

package galley

import (
	"istio.io/istio/pkg/test/framework2/components/environment/kube"
	"testing"

	"istio.io/istio/pkg/test/framework2/core"

	"istio.io/istio/pkg/test/framework2/components/environment/native"
)

// Instance of Galley
type Instance interface {
	core.Resource

	// Address of the Galley MCP Server.
	Address() string

	// ApplyConfig applies the given config yaml file via Galley.
	ApplyConfig(ns core.Namespace, yamlText ...string) error

	// ApplyConfigOrFail applies the given config yaml file via Galley.
	ApplyConfigOrFail(t *testing.T, ns core.Namespace, yamlText ...string)

	// ApplyConfigDir recursively applies all the config files in the specified directory
	ApplyConfigDir(configDir string) error

	// ClearConfig clears all applied config so far.
	ClearConfig() error

	// WaitForSnapshot waits until the given snapshot is observed for the given type URL.
	WaitForSnapshot(collection string, snapshot ...map[string]interface{}) error
}

// Configuration for Galley
type Config struct {
	// MeshConfig to use for this instance.
	MeshConfig string
}

// New returns a new Galley instance.
func New(c core.Context, cfg Config) (Instance, error) {
	switch c.Environment().EnvironmentName() {
	case core.Native:
		return newNative(c, c.Environment().(*native.Environment), cfg)
	case core.Kube:
		return newKube(c, c.Environment().(*kube.Environment), cfg)
	default:
		return nil, core.UnsupportedEnvironment(c.Environment().EnvironmentName())
	}
}

// NewOrFail returns a new Galley instance, or fails test.
func NewOrFail(t *testing.T, c core.Context, cfg Config) Instance {
	t.Helper()

	i, err := New(c, cfg)
	if err != nil {
		t.Fatalf("galley.NewOrFail: %v", err)
	}
	return i
}
