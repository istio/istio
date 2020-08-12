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

package multicluster

import (
	"fmt"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	retryTimeout = time.Second * 30
	retryDelay   = time.Millisecond * 100
)

type Apps struct {
	Namespace namespace.Instance

	// UniqueEchos are have different names in each cluster.
	UniqueEchos echo.Instances
	// LBEchos are the same in each cluster
	LBEchos echo.Instances
}

func SetupApps(ctx resource.Context) (*Apps, error) {
	apps := &Apps{}

	var err error
	apps.Namespace, err = namespace.New(ctx, namespace.Config{Prefix: "mc-reachability", Inject: true})
	if err != nil {
		return nil, err
	}

	// set up echos
	// Running multiple instances in each cluster teases out cases where proxies inconsistently
	// use wrong different discovery server. For higher numbers of clusters, we already end up
	// running plenty of services. (see https://github.com/istio/istio/issues/23591).
	uniqSvcPerCluster := 5 - len(ctx.Clusters())
	if uniqSvcPerCluster < 1 {
		uniqSvcPerCluster = 1
	}

	builder := echoboot.NewBuilder(ctx)
	for _, cluster := range ctx.Clusters() {
		builder.With(nil, newEchoConfig("echolb", apps.Namespace, cluster))
		for i := 0; i < uniqSvcPerCluster; i++ {
			svcName := fmt.Sprintf("echo-%d-%d", cluster.Index(), i)
			builder = builder.With(nil, newEchoConfig(svcName, apps.Namespace, cluster))
		}
	}
	echos, err := builder.Build()
	if err != nil {
		return nil, err
	}
	apps.UniqueEchos = echos.Match(echo.ServicePrefix("echo-"))
	apps.LBEchos = echos.Match(echo.Service("echolb"))

	return apps, nil
}

func newEchoConfig(service string, ns namespace.Instance, cluster resource.Cluster) echo.Config {
	return echo.Config{
		Service:        service,
		Namespace:      ns,
		Cluster:        cluster,
		ServiceAccount: true,
		Subsets: []echo.SubsetConfig{
			{
				Version: "v1",
			},
		},
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
			{
				Name:     "tcp",
				Protocol: protocol.TCP,
			},
			{
				Name:     "grpc",
				Protocol: protocol.GRPC,
			},
		},
	}
}

func callOrFail(ctx test.Failer, src, dest echo.Instance) client.ParsedResponses {
	ctx.Helper()
	var results client.ParsedResponses
	retry.UntilSuccessOrFail(ctx, func() (err error) {
		results, err = src.Call(echo.CallOptions{
			Target:   dest,
			PortName: "http",
			Scheme:   scheme.HTTP,
			Count:    15,
		})
		if err == nil {
			err = results.CheckOK()
		}
		if err != nil {
			return fmt.Errorf("%s to %s:%s using %s: expected success but failed: %v",
				src.Config().Service, dest.Config().Service, "http", scheme.HTTP, err)
		}
		return nil
	}, retry.Timeout(retryTimeout), retry.Delay(retryDelay))
	return results
}
