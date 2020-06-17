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

package registry_test

import (
	"reflect"
	"testing"

	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/mixer"
	"istio.io/istio/pilot/pkg/networking/plugin/registry"
)

func TestPlugins(t *testing.T) {
	expectedPlugins := []string{"mixer"}
	plugins := registry.NewPlugins(expectedPlugins)
	if len(plugins) != len(expectedPlugins) {
		t.Errorf("expected length of plugins to be %d, but got %d", len(expectedPlugins), len(plugins))
	}

	var checkPluginType = func(i int, p func() plugin.Plugin) {
		if reflect.TypeOf(plugins[i]) != reflect.TypeOf(p()) {
			t.Errorf("expected type of plugin to be %s, but got %s", reflect.TypeOf(plugins[i]), reflect.TypeOf(p()))
		}
	}

	checkPluginType(0, mixer.NewPlugin)
}

func TestPluginsNonValid(t *testing.T) {
	expectedPlugins := []string{"abc"}
	plugins := registry.NewPlugins(expectedPlugins)
	if len(plugins) != 0 {
		t.Errorf("expected length of plugins to be %d, but got %d", 0, len(plugins))
	}
}
