package kube

import (
	"regexp"
	"testing"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/core/image"
)

var (
	settings *image.Settings = &image.Settings{
		Hub:        "testing.hub",
		Tag:        "latest",
		PullPolicy: "Always",
	}
)

var (
	statusPattern     = regexp.MustCompile("sidecar.istio.io/status: '{\"version\":\"([0-9a-f]+)\",")
	statusReplacement = "sidecar.istio.io/status: '{\"version\":\"\","
)

func stripVersion(yaml []byte) []byte {
	return statusPattern.ReplaceAllLiteral(yaml, []byte(statusReplacement))
}

func TestDeploymentYAML(t *testing.T) {

	testCase := []struct {
		name         string
		wantFilePath string
		config       echo.Config
	}{
		{
			name:         "basic",
			wantFilePath: "testdata/basic.yaml",
			config: echo.Config{
				Service: "foo",
				Version: "bar",
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8090,
						ServicePort:  8090,
					},
				},
			},
		},
		{
			name:         "two-workloads-one-nosidecar",
			wantFilePath: "testdata/two-workloads-one-nosidecar.yaml",
			config: echo.Config{
				Service: "foo",
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8090,
						ServicePort:  8090,
					},
				},
				Workloads: []echo.WorkloadConfig{
					{
						Version: "v1",
					},
					{
						Version:     "nosidecar",
						Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
					},
				},
			},
		},
		{
			name:         "multiversion",
			wantFilePath: "testdata/multiversion.yaml",
			config: echo.Config{
				Service: "multiversion",
				Workloads: []echo.WorkloadConfig{
					{
						Name: "istio",
					},
					{
						Name:        "legacy",
						Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
					},
				},
				Ports: []echo.Port{
					{
						Name:         "http",
						Protocol:     protocol.HTTP,
						InstancePort: 8090,
						ServicePort:  8090,
					},
					{
						Name:         "tcp",
						Protocol:     protocol.TCP,
						InstancePort: 9000,
						ServicePort:  9000,
					},
					{
						Name:         "grpc",
						Protocol:     protocol.GRPC,
						InstancePort: 9090,
						ServicePort:  9090,
					},
				},
			},
		},
	}
	for _, tc := range testCase {
		yaml, err := generateYAMLWithSettings(tc.config, settings)
		if err != nil {
			t.Errorf("failed to generate yaml %v", err)
		}
		gotBytes := []byte(yaml)
		wantedBytes := testutil.ReadGoldenFile(gotBytes, tc.wantFilePath, t)

		wantBytes := testutil.StripVersion(wantedBytes)
		gotBytes = testutil.StripVersion(gotBytes)

		testutil.CompareBytes(gotBytes, wantBytes, tc.wantFilePath, t)

		if testutil.Refresh() {
			testutil.RefreshGoldenFile(gotBytes, tc.wantFilePath, t)
		}
	}
}
