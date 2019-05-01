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
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance of Galley
type Instance interface {
	resource.Resource

	// Address of the Galley MCP Server.
	Address() string

	// ApplyConfig applies the given config yaml text via Galley.
	ApplyConfig(ns namespace.Instance, yamlText ...string) error

	// ApplyConfigOrFail applies the given config yaml text via Galley.
	ApplyConfigOrFail(t *testing.T, ns namespace.Instance, yamlText ...string)

	// DeleteConfig deletes the given config yaml text via Galley.
	DeleteConfig(ns namespace.Instance, yamlText ...string) error

	// DeleteConfigOrFail deletes the given config yaml text via Galley.
	DeleteConfigOrFail(t *testing.T, ns namespace.Instance, yamlText ...string)

	// ApplyConfigDir recursively applies all the config files in the specified directory
	ApplyConfigDir(ns namespace.Instance, configDir string) error

	// ClearConfig clears all applied config so far.
	ClearConfig() error

	// WaitForSnapshot waits until the given snapshot is observed for the given type URL.
	WaitForSnapshot(collection string, validator SnapshotValidatorFunc) error

	// WaitForSnapshotOrFail calls WaitForSnapshot and fails the test if it fails.
	WaitForSnapshotOrFail(t *testing.T, collection string, validator SnapshotValidatorFunc)
}

// Config for Galley
type Config struct {

	// SinkAddress to dial-out to, if set.
	SinkAddress string

	// MeshConfig to use for this instance.
	MeshConfig string
}

// New returns a new instance of echo.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Native, func() {
		i, err = newNative(ctx, cfg)
	})
	ctx.Environment().Case(environment.Kube, func() {
		i, err = newKube(ctx, cfg)
	})
	return
}

// NewOrFail returns a new Galley instance, or fails test.
func NewOrFail(t *testing.T, c resource.Context, cfg Config) Instance {
	t.Helper()

	i, err := New(c, cfg)
	if err != nil {
		t.Fatalf("galley.NewOrFail: %v", err)
	}
	return i
}
