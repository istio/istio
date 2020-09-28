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

package multicluster

import (
	"fmt"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	retryTimeout = time.Second * 10
	retryDelay   = time.Millisecond * 100
)

type AppContext struct {
	apps
	Namespace      namespace.Instance
	LocalNamespace namespace.Instance
	// ControlPlaneValues to set during istio deploy
	ControlPlaneValues string
}

type apps struct {
	// UniqueEchos are have different names in each cluster.
	UniqueEchos echo.Instances
	// LBEchos are the same in each cluster
	LBEchos echo.Instances
	// LocalEchos can only be reached from the same cluster
	LocalEchos echo.Instances
}

// Setup initialize app context with control plane values and namespaces
func Setup(appCtx *AppContext) resource.SetupFn {
	return func(ctx resource.Context) error {
		*appCtx = AppContext{}
		var err error
		appCtx.Namespace, err = namespace.New(ctx, namespace.Config{Prefix: "mc-reachability", Inject: true})
		if err != nil {
			return err
		}
		appCtx.LocalNamespace, err = namespace.New(ctx, namespace.Config{Prefix: "cluster-local", Inject: true})
		if err != nil {
			return err
		}
		appCtx.ControlPlaneValues = fmt.Sprintf(`
values:
  meshConfig:
    serviceSettings: 
      - settings:
          clusterLocal: true
        hosts:
          - "*.%s.svc.cluster.local"
`, appCtx.LocalNamespace.Name())
		return nil
	}
}

// SetupApps depoys echos
func SetupApps(appCtx *AppContext) resource.SetupFn {
	return func(ctx resource.Context) error {
		if appCtx.Namespace == nil || appCtx.LocalNamespace == nil {
			return fmt.Errorf("namespaces not initialized; run Setup first")
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
			echoLbCfg := newEchoConfig("echolb", appCtx.Namespace, cluster)
			echoLbCfg.Subsets = append(echoLbCfg.Subsets, echo.SubsetConfig{Version: "v2"})

			builder.With(nil, echoLbCfg)
			builder.With(nil, newEchoConfig("local", appCtx.LocalNamespace, cluster))
			for i := 0; i < uniqSvcPerCluster; i++ {
				svcName := fmt.Sprintf("echo-%d-%d", cluster.Index(), i)
				builder = builder.With(nil, newEchoConfig(svcName, appCtx.Namespace, cluster))
			}
		}
		echos, err := builder.Build()
		if err != nil {
			return err
		}

		appCtx.apps = apps{
			UniqueEchos: echos.Match(echo.ServicePrefix("echo-")),
			LBEchos:     echos.Match(echo.Service("echolb")),
			// it's faster to spin up cluster-local echos than to wait for creating/deleting cluster-local config to propagate
			LocalEchos: echos.Match(echo.Service("local")),
		}
		return nil
	}
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

type callChecker func(client.ParsedResponses) error

func callOrFail(ctx framework.TestContext, src, dest echo.Instance, checkers ...callChecker) {
	ctx.Helper()
	var results client.ParsedResponses
	retry.UntilSuccessOrFail(ctx, func() (err error) {
		results, err = src.Call(echo.CallOptions{
			Target:   dest,
			PortName: "http",
			Scheme:   scheme.HTTP,
			Count:    20 * len(ctx.Clusters()),
		})

		checkers = append([]callChecker{func(responses client.ParsedResponses) error {
			return responses.CheckOK()
		}}, checkers...)

		if err == nil {
			for _, c := range checkers {
				err = c(results)
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			return fmt.Errorf("%s to %s:%s using %s: %v",
				src.Config().Service, dest.Config().Service, "http", scheme.HTTP, err)
		}
		return nil
	}, retry.Timeout(retryTimeout), retry.Delay(retryDelay))
}
