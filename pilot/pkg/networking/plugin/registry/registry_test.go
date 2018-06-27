package registry_test

import (
	"reflect"
	"testing"

	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/plugin/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/plugin/health"
	"istio.io/istio/pilot/pkg/networking/plugin/mixer"
	"istio.io/istio/pilot/pkg/networking/plugin/registry"
)

func TestPlugins(t *testing.T) {
	expectedPlugins := []string{"mixer", "health", "envoyfilter"}
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
	checkPluginType(1, health.NewPlugin)
	checkPluginType(2, envoyfilter.NewPlugin)
}

func TestPluginsNonValid(t *testing.T) {
	expectedPlugins := []string{"abc"}
	plugins := registry.NewPlugins(expectedPlugins)
	if len(plugins) != 0 {
		t.Errorf("expected length of plugins to be %d, but got %d", 0, len(plugins))
	}
}
