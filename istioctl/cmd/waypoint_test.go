package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
)

func TestWaypointList(t *testing.T) {
	cases := []struct {
		name            string
		args            []string
		gateways        []*gateway.Gateway
		expectedOutFile string
	}{
		{
			name:            "no gateways",
			args:            strings.Split("x waypoint list", " "),
			gateways:        []*gateway.Gateway{},
			expectedOutFile: "no-gateway",
		},
		{
			name: "default namespace gateway",
			args: strings.Split("x waypoint list  -n default", " "),
			gateways: []*gateway.Gateway{
				makeGateway("namespace", "default", "", true, true),
				makeGateway("namespace", "fake", "", true, true),
			},
			expectedOutFile: "default-gateway",
		},
		{
			name: "all namespaces gateways",
			args: strings.Split("x waypoint list -A", " "),
			gateways: []*gateway.Gateway{
				makeGateway("namespace", "default", "", true, true),
				makeGateway("namespace", "fake", "", true, true),
			},
			expectedOutFile: "all-gateway",
		},
		{
			name: "have both managed and unmanaged gateways",
			args: strings.Split("x waypoint list -A", " "),
			gateways: []*gateway.Gateway{
				makeGateway("bookinfo", "default", "bookinfo", false, false),
				makeGateway("bookinfo-invalid", "fake", "bookinfo", true, true),
				makeGateway("namespace", "default", "", false, true),
				makeGateway("bookinfo-valid", "bookinfo", "bookinfo-valid", true, true),
			},
			expectedOutFile: "combined-gateway",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client := kube.NewFakeClient()
			kubeClient = func(kubeconfig, configContext string) (kube.CLIClient, error) {
				return client, nil
			}
			for _, gw := range tt.gateways {
				_, _ = client.GatewayAPI().GatewayV1beta1().Gateways(gw.Namespace).Create(context.Background(), gw, metav1.CreateOptions{})
			}
			defaultFile, err := os.ReadFile(fmt.Sprintf("testdata/waypoint/%s", tt.expectedOutFile))
			if err != nil {
				t.Fatal(err)
			}
			expectedOut := string(defaultFile)
			if len(expectedOut) == 0 {
				t.Fatal("expected output is empty")
			}

			var out bytes.Buffer
			rootCmd := GetRootCmd(tt.args)
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&out)

			fErr := rootCmd.Execute()
			if fErr != nil {
				t.Fatal(fErr)
			}
			output := out.String()
			if output != expectedOut {
				t.Fatalf("expected %s, got %s", expectedOut, output)
			}
		})
	}
}

func makeGateway(name, namespace, sa string, programmed, ready bool) *gateway.Gateway {
	conditions := make([]metav1.Condition, 0)
	if programmed {
		conditions = append(conditions, metav1.Condition{
			Type:   string(gateway.GatewayConditionProgrammed),
			Status: kstatus.StatusTrue,
		})
	} else {
		conditions = append(conditions, metav1.Condition{
			Type:   string(gateway.GatewayConditionProgrammed),
			Status: kstatus.StatusFalse,
		})
	}
	if ready {
		conditions = append(conditions, metav1.Condition{
			Type:   string(gateway.GatewayConditionReady),
			Status: kstatus.StatusTrue,
		})
	} else {
		conditions = append(conditions, metav1.Condition{
			Type:   string(gateway.GatewayConditionReady),
			Status: kstatus.StatusFalse,
		})
	}
	return &gateway.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				constants.WaypointServiceAccount: sa,
			},
		},
		Status: gateway.GatewayStatus{
			Conditions: conditions,
		},
	}
}
