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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"

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
				makeGateway("namespace", "default", "", true, true),
				makeGateway("namespace", "fake", "", true, true),
			},
			expectedOutFile: "default-gateway",
		},
		{
			name: "all namespaces gateways",
			args: strings.Split("list -A", " "),
			gateways: []*gateway.Gateway{
				makeGateway("namespace", "default", "", true, true),
				makeGateway("namespace", "fake", "", true, true),
			},
			expectedOutFile: "all-gateway",
		},
		{
			name: "have both managed and unmanaged gateways",
			args: strings.Split("list -A", " "),
			gateways: []*gateway.Gateway{
				makeGateway("bookinfo", "default", "bookinfo", false, true),
				makeGateway("bookinfo-invalid", "fake", "bookinfo", true, false),
				makeGateway("namespace", "default", "", false, true),
				makeGateway("bookinfo-valid", "bookinfo", "bookinfo-valid", true, true),
				makeGateway("no-name-convention", "default", "sa", true, true),
				makeGatewayWithRevision("bookinfo-rev", "bookinfo", "bookinfo-rev", true, true, "rev1"),
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

func makeGateway(name, namespace, sa string, programmed, isWaypoint bool) *gateway.Gateway {
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
			Annotations: map[string]string{
				constants.WaypointServiceAccount: sa,
			},
		},
		Spec: gateway.GatewaySpec{
			GatewayClassName: gateway.ObjectName(className),
		},
		Status: gateway.GatewayStatus{
			Conditions: conditions,
		},
	}
}

func makeGatewayWithRevision(name, namespace, sa string, programmed, isWaypoint bool, rev string) *gateway.Gateway {
	gw := makeGateway(name, namespace, sa, programmed, isWaypoint)
	if gw.Labels == nil {
		gw.Labels = make(map[string]string)
	}
	gw.Labels[label.IoIstioRev.Name] = rev
	return gw
}
