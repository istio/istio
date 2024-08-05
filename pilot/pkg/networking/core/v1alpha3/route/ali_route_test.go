package route

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

func TestSupportFallback(t *testing.T) {
	testCases := []struct {
		input  *model.Proxy
		expect bool
	}{
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			expect: false,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.20.7",
					},
				},
			},
			expect: true,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.20.6",
					},
				},
			},
			expect: false,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.19.6",
					},
				},
			},
			expect: false,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.21.6",
					},
				},
			},
			expect: true,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						"envoy_version": "1.21.6",
					},
				},
			},
			expect: false,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.21.6-test",
					},
				},
			},
			expect: true,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.21.6-test-v1-foo",
					},
				},
			},
			expect: true,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.21.6-test" + serverlessSuffix,
					},
				},
			},
			expect: true,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.21.6-test-v1-foo" + serverlessSuffix,
					},
				},
			},
			expect: true,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.21.6" + serverlessSuffix,
					},
				},
			},
			expect: true,
		},
		{
			input: &model.Proxy{
				Metadata: &model.NodeMetadata{
					Raw: map[string]any{
						envoyVersion: "1.19.6" + serverlessSuffix,
					},
				},
			},
			expect: false,
		},
	}

	for _, testCase := range testCases {
		t.Run("", func(t *testing.T) {
			if supportFallback(testCase.input) != testCase.expect {
				t.Fatal("should be equal")
			}
		})
	}
}
