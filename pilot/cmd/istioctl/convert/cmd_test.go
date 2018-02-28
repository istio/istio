package convert

import (
	"io"
	"os"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestCommand(t *testing.T) {
	tt := []struct {
		in  []string
		out string
	}{
		{in: []string{"rule-default-route.yaml"},
			out: "rule-default-route.yaml"},

		{in: []string{"rule-weighted-route.yaml"},
			out: "rule-weighted-route.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-content-route.yaml"},
			out: "rule-content-route.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-regex-route.yaml"},
			out: "rule-regex-route.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-fault-injection.yaml"},
			out: "rule-fault-injection.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-redirect-injection.yaml"},
			out: "rule-redirect-injection.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-websocket-route.yaml"},
			out: "rule-websocket-route.yaml"},

		{in: []string{"rule-default-route-append-headers.yaml"},
			out: "rule-default-route-append-headers.yaml"},

		{in: []string{"rule-default-route-cors-policy.yaml"},
			out: "rule-default-route-cors-policy.yaml"},

		{in: []string{"rule-default-route-mirrored.yaml"},
			out: "rule-default-route-mirrored.yaml"},
	}

	for _, tc := range tt {
		t.Run(tc.out, func(t *testing.T) {
			readers := make([]io.Reader, 0, len(tc.in))
			for _, filename := range tc.in {
				file, err := os.Open("testdata/v1alpha1/" + filename)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close() // nolint: errcheck
				readers = append(readers, file)
			}

			outFilename := "testdata/v1alpha2/" + tc.out
			out, err := os.Create(outFilename)
			if err != nil {
				t.Fatal(err)
			}
			// FIXME: close before CompareYAML?
			defer out.Close() // nolint: errcheck

			if err := convertConfigs(readers, out); err != nil {
				t.Fatalf("Unexpected error converting configs: %v", err)
			}

			util.CompareYAML(outFilename, t)
		})
	}
}
