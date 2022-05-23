package filters

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"istio.io/istio/pilot/pkg/networking/util"
)

func TestBuildRouterFilter(t *testing.T) {
	tests := []struct {
		name     string
		ctx      *RouterFilterContext
		expected *hcm.HttpFilter
	}{
		{
			name: "test for build router filter",
			ctx:  &RouterFilterContext{StartChildSpan: true},
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: util.MessageToAny(&router.Router{
						StartChildSpan: true,
					}),
				},
			},
		},
		{
			name: "test for build router filter with start child span false",
			ctx:  &RouterFilterContext{StartChildSpan: false},
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: util.MessageToAny(&router.Router{
						StartChildSpan: false,
					}),
				},
			},
		},
		{
			name: "test for build router filter with empty context",
			ctx:  nil,
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: util.MessageToAny(&router.Router{}),
				},
			},
		},
	}

	for _, tt := range tests {
		result := BuildRouterFilter(tt.ctx)
		if result.GetTypedConfig() != tt.expected.GetTypedConfig() {
			t.Errorf("Test %s failed, expected: %v ,got: %v", tt.name, spew.Sdump(result), spew.Sdump(tt.expected))
		}
	}
}
