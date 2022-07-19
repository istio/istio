//go:build integ
// +build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package dnsautoallocation

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/util/retry"
)

type testRequestSpec struct {
	protocol   protocol.Instance
	portName   string
	portNumber int
}

var requestsSpec = []testRequestSpec{
	{
		portNumber: 80,
		portName:   ports.HTTP,
		protocol:   protocol.HTTP,
	},
	{
		portNumber: 443,
		portName:   ports.HTTPS,
		protocol:   protocol.HTTPS,
	},
}

const serviceEntryFile = "testdata/service-entry-tmpl.yaml"
const externalHost = "istio.io"

func TestMeshExternalServiceEntry(t *testing.T) {
	// nolint: staticcheck
	framework.
		NewTest(t).
		Features("traffic.dnscautoallocation").
		Run(func(ctx framework.TestContext) {
			meshNs := apps.A.NamespaceName()
			client := apps.A[0]
			server := apps.External.All[0]
			// externalHost := server.ClusterLocalFQDN()

			testRequest(ctx, "without-applying-any-service-entry", client, server)

			httpSe := ctx.ConfigIstio().EvalFile(meshNs, map[string]interface{}{
				"externalHost": externalHost,
				"portNumber":   80,
				"portName":     ports.HTTP,
				"portProtocol": protocol.HTTP,
			}, serviceEntryFile)

			httpSe.ApplyOrFail(ctx)

			// testRequest(ctx, "after-applying-only-http-service-entry", client, server)

			httpsSe := ctx.ConfigIstio().EvalFile(meshNs, map[string]interface{}{
				"externalHost": externalHost,
				"portNumber":   443,
				"portName":     ports.HTTPS,
				"portProtocol": protocol.HTTPS,
			}, serviceEntryFile)

			httpsSe.ApplyOrFail(ctx)

			testRequest(ctx, "after-applying-both-https-and-http-service-entry", client, server)
		})
}

func testRequest(ctx framework.TestContext, status string, client, server echo.Instance) {
	for _, spec := range requestsSpec {
		testName := fmt.Sprintf("%s-request-%s", spec.portName, status)

		ctx.NewSubTest(testName).Run(func(ctx framework.TestContext) {
			retry.UntilSuccessOrFail(ctx, func() error {

				if err := testConnectivity(client, server, spec.protocol, spec.portName, spec.portNumber, testName); err != nil {
					return err
				}

				return nil
			}, retry.Timeout(10*time.Second))
		})
	}
}

func testConnectivity(from, to echo.Instance, p protocol.Instance, portName string, portNumber int, testName string) error {
	res, err := from.Call(echo.CallOptions{
		Address: externalHost,
		Port: echo.Port{
			ServicePort: portNumber,
			Name:        portName,
			Protocol:    p,
		},
	})

	if err != nil {
		return fmt.Errorf("failed to request to external service: %s", err)
	}

	code, err := strconv.Atoi(res.Responses[0].Code)

	if err == nil && code >= 500 {
		return fmt.Errorf("expected to get server status code, got: %s", res.Responses[0].Code)
	}

	return nil
}
