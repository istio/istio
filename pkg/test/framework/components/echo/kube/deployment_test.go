package kube

import (
	"testing"

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

func TestDeploymentYAML(t *testing.T) {
	cfg := echo.Config{
		Service: "foo",
		Workloads: []echo.WorkloadConfig{
			{
				Version: "v1",
			},
			{
				Version:     "nosidecar",
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
			},
		},
	}
	_, err := generateYAMLWithSettings(cfg, settings)
	if err != nil {
		t.Errorf("failed to generate yaml %v", err)
	}
}

func TestLegacyConfig(t *testing.T) {
	cfg := echo.Config{
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
	}
	_, err := generateYAMLWithSettings(cfg, settings)
	if err != nil {
		t.Errorf("failed to generate yaml %v", err)
	}
}
