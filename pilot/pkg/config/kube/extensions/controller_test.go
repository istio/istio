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

package extensions

import (
	"fmt"
	"testing"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/retry"
)

func TestTranslation(t *testing.T) {
	// Create a memory store
	store := memory.NewController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))

	// Create a WasmPlugin config
	wasmPlugin := config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WasmPlugin,
			Name:             "test-plugin",
			Namespace:        "default",
		},
		Spec: &extensions.WasmPlugin{
			Phase: extensions.PluginPhase_AUTHN,
			Url:   "oci://example.com/filter:v1",
		},
	}

	// Add the WasmPlugin to the store BEFORE running
	_, err := store.Create(wasmPlugin)
	if err != nil {
		t.Fatalf("failed to create WasmPlugin: %v", err)
	}

	// Create translation controller
	controller := NewController(store, nil, nil)

	// Start the memory store
	storeStop := make(chan struct{})
	t.Cleanup(func() { close(storeStop) })
	go store.Run(storeStop)

	// Start the controller
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go controller.Run(stop)

	// Wait for controller to sync
	retry.UntilSuccessOrFail(t, func() error {
		if !controller.HasSynced() {
			return fmt.Errorf("controller not synced yet")
		}
		return nil
	})

	// Wait for translation (KRT collections are async)
	var translated []config.Config
	retry.UntilSuccessOrFail(t, func() error {
		translated = controller.List(gvk.ExtensionFilter, "")
		if len(translated) == 0 {
			return fmt.Errorf("no translated configs yet")
		}
		return nil
	}, retry.Timeout(retry.DefaultTimeout))

	if err != nil || len(translated) != 1 {
		t.Fatalf("expected 1 translated ExtensionFilter, got %d", len(translated))
	}

	// Verify the translated config
	ef := translated[0]
	if ef.Name != "test-plugin"+syntheticMarker {
		t.Errorf("expected name %s, got %s", "test-plugin"+syntheticMarker, ef.Name)
	}
	if ef.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", ef.Namespace)
	}
	if ef.GroupVersionKind != gvk.ExtensionFilter {
		t.Errorf("expected GVK ExtensionFilter, got %v", ef.GroupVersionKind)
	}
}
