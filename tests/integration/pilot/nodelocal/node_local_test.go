//go:build integ
// +build integ

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

package nodelocal

import (
	"errors"
	"testing"

	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	pilotcommont "istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/tests/integration/pilot/common"
)

func TestNodeLocal(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.k8s-internal-traffic").
		RequiresMinClusters(2).
		Run(func(t framework.TestContext) {
			apps.Namespace, _ = namespace.New(t, namespace.Config{
				Prefix: "node-local",
				Inject: true,
			})

			echos, _ = echoboot.NewBuilder(t).
				WithClusters(t.Clusters()...).
				WithConfig(echo.Config{
					Service:   common.PodASvc,
					Namespace: apps.Namespace,
					Ports:     pilotcommont.EchoPorts,
				}).Build()

			trafficCase := trafficTestCase(false)
			trafficCase.RunForApps(t, echos, apps.Namespace.Name())

			// When PodASvc sets `InternalTrafficPolicy=Local`, it only can be accessed by the same node instance.
			echos, _ = echoboot.NewBuilder(t).
				WithClusters(t.Clusters()...).
				WithConfig(echo.Config{
					Service:               common.PodASvc,
					Namespace:             apps.Namespace,
					Ports:                 pilotcommont.EchoPorts,
					InternalTrafficPolicy: "Local",
				}).Build()

			trafficCase = trafficTestCase(true)
			trafficCase.RunForApps(t, echos, apps.Namespace.Name())
		})
}

func trafficTestCase(nodeLocal bool) common.TrafficTestCase {
	return common.TrafficTestCase{
		Opts: echo.CallOptions{PortName: "http"},
		Validate: func(src echo.Caller, dst echo.Instances) echo.Validator {
			return validResponse(src.(echo.Instance).Config().Cluster.Name(), src.(echo.Instance), nodeLocal)
		},
	}
}

func validResponse(expectCluster string, src echo.Instance, nodeLocal bool) echo.Validator {
	return echo.ValidatorFunc(func(responses client.ParsedResponses, _ error) error {
		var srcNodeName string
		if src != nil {
			srcNodeNames, _ := src.Workloads()
			if len(srcNodeNames) != 0 {
				srcNodeName = srcNodeNames[0].NodeName()
			}
		}
		sameNode := sameNode(responses, srcNodeName, expectCluster)
		if (sameNode || !nodeLocal) && responses.CheckCode("200") == nil {
			return nil
		}
		if (!sameNode && nodeLocal) && responses.CheckCode("503") == nil {
			return nil
		}
		return errors.New("unexpect response code")
	})
}

func sameNode(responses client.ParsedResponses, srcNodeName, cluster string) bool {
	if responses.CheckCluster(cluster) != nil {
		return false
	}
	if responses.CheckNodeName(srcNodeName) != nil {
		return false
	}
	return true
}
