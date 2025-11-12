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

package waypoint

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/constants"
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
			args:            strings.Split("list", " "),
			gateways:        []*gateway.Gateway{},
			expectedOutFile: "no-gateway",
		},
		{
			name: "default namespace gateway",
			args: strings.Split("list", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", true, true),
				makeGateway(constants.DefaultNamespaceWaypoint, "fake", true, true),
			},
			expectedOutFile: "default-gateway",
		},
		{
			name: "all namespaces gateways",
			args: strings.Split("list -A", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", true, true),
				makeGateway(constants.DefaultNamespaceWaypoint, "fake", true, true),
			},
			expectedOutFile: "all-gateway",
		},
		{
			name: "have both managed and unmanaged gateways",
			args: strings.Split("list -A", " "),
			gateways: []*gateway.Gateway{
				makeGateway("bookinfo", "default", false, true),
				makeGateway("bookinfo-invalid", "fake", true, false),
				makeGateway(constants.DefaultNamespaceWaypoint, "default", false, true),
				makeGateway("bookinfo-valid", "bookinfo", true, true),
				makeGateway("no-name-convention", "default", true, true),
				makeGatewayWithRevision("bookinfo-rev", "bookinfo", true, true, "rev1"),
				makeGatewayWithTrafficType("bookinfo-traffic-type", "bookinfo", true, true, constants.AllTraffic),
			},
			expectedOutFile: "combined-gateway",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
				Namespace: "default",
			})
			client, err := ctx.CLIClient()
			if err != nil {
				t.Fatal(err)
			}

			for _, gw := range tt.gateways {
				_, _ = client.GatewayAPI().GatewayV1().Gateways(gw.Namespace).Create(context.Background(), gw, metav1.CreateOptions{})
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
			rootCmd := Cmd(ctx)
			rootCmd.SetArgs(tt.args)
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

func TestWaypointStatus(t *testing.T) {
	cases := []struct {
		name            string
		args            []string
		gateways        []*gateway.Gateway
		expectedOutFile string
	}{
		{
			name: "waypoint ready",
			args: strings.Split("status", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", true, true),
			},
			expectedOutFile: "waypoint-status-ready",
		},
		{
			name: "waypoint ready with --wait",
			args: strings.Split("status --wait=true", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", true, true),
			},
			expectedOutFile: "waypoint-status-ready",
		},
		{
			name: "waypoint ready with --wait=false",
			args: strings.Split("status --wait=false", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", true, true),
			},
			expectedOutFile: "waypoint-status-ready",
		},
		{
			name: "waypoint not ready without --wait",
			args: strings.Split("status --waypoint-timeout 0.5s", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", false, true),
			},
			expectedOutFile: "waypoint-notready-wait",
		},
		{
			name: "waypoint not ready with --wait",
			args: strings.Split("status --wait --waypoint-timeout 0.5s", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", false, true),
			},
			expectedOutFile: "waypoint-notready-wait",
		},
		{
			name: "waypoint not ready with --wait=false",
			args: strings.Split("status --wait=false --waypoint-timeout 0.5s", " "),
			gateways: []*gateway.Gateway{
				makeGateway(constants.DefaultNamespaceWaypoint, "default", false, true),
			},
			expectedOutFile: "waypoint-status-notready",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := cli.NewFakeContext(&cli.NewFakeContextOption{
				Namespace: "default",
			})
			client, err := ctx.CLIClient()
			if err != nil {
				t.Fatal(err)
			}

			for _, gw := range tt.gateways {
				_, _ = client.GatewayAPI().GatewayV1().Gateways(gw.Namespace).Create(context.Background(), gw, metav1.CreateOptions{})
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
			rootCmd := Cmd(ctx)
			rootCmd.SetArgs(tt.args)
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&out)
			// disable Usage
			rootCmd.SetUsageFunc(func(cmd *cobra.Command) error {
				return nil
			})

			rootCmd.Execute()
			output := out.String()
			if output != expectedOut {
				t.Fatalf("expected %s, got %s", expectedOut, output)
			}
		})
	}
}

func makeGateway(name, namespace string, programmed, isWaypoint bool) *gateway.Gateway {
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
	className := "other"
	if isWaypoint {
		className = constants.WaypointGatewayClassName
	}
	return &gateway.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gateway.GatewaySpec{
			GatewayClassName: gateway.ObjectName(className),
		},
		Status: gateway.GatewayStatus{
			Conditions: conditions,
		},
	}
}

func makeGatewayWithRevision(name, namespace string, programmed, isWaypoint bool, rev string) *gateway.Gateway {
	gw := makeGateway(name, namespace, programmed, isWaypoint)
	if gw.Labels == nil {
		gw.Labels = make(map[string]string)
	}
	gw.Labels[label.IoIstioRev.Name] = rev
	return gw
}

func makeGatewayWithTrafficType(name, namespace string, programmed, isWaypoint bool, trafficType string) *gateway.Gateway {
	gw := makeGateway(name, namespace, programmed, isWaypoint)
	if gw.Labels == nil {
		gw.Labels = make(map[string]string)
	}
	gw.Labels[label.IoIstioWaypointFor.Name] = trafficType
	return gw
}
