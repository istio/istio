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

package upgrade

import (
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

type VersionedEchoDeployments struct {
	// Echo instances running master version
	Latest   echo.Instances
	LatestNs namespace.Instance

	// Echo instances running tip of previous patch version
	NMinusOne   echo.Instances
	NMinusOneNs namespace.Instance

	// Echo instances running
	NMinusTwo   echo.Instances
	NMinusTwoNs namespace.Instance

	// All echo instances
	All echo.Instances
}

const (
	LatestSvc    = "latest"
	NMinusOneSvc = "n-1"
	NMinusTwoSvc = "n-2"
)

var VersionedEchoPorts = []echo.Port{
	{Name: "http", Protocol: protocol.HTTP, ServicePort: 80, InstancePort: 18080},
	{Name: "grpc", Protocol: protocol.GRPC, ServicePort: 7070, InstancePort: 17070},
	{Name: "tcp", Protocol: protocol.TCP, ServicePort: 9090, InstancePort: 19090},
}

func SetupApps(ctx resource.Context, latest, nMinusOne, nMinusTwo istio.Instance, apps *VersionedEchoDeployments) error {
	var err error
	apps.LatestNs, err = namespace.New(ctx, namespace.Config{
		Prefix:   "echo-latest-",
		Revision: latest.Settings().Revision,
		Inject:   true,
	})
	if err != nil {
		return err
	}
	apps.NMinusOneNs, err = namespace.New(ctx, namespace.Config{
		Prefix:   "echo-n-1",
		Revision: nMinusOne.Settings().Revision,
		Inject:   true,
	})
	if err != nil {
		return err
	}
	apps.NMinusTwoNs, err = namespace.New(ctx, namespace.Config{
		Prefix:   "echo-n-2",
		Revision: nMinusTwo.Settings().Revision,
		Inject:   true,
	})
	if err != nil {
		return err
	}

	for _, p := range VersionedEchoPorts {
		p.ServicePort = p.InstancePort
	}

	c := ctx.Clusters().Default()
	builder := echoboot.NewBuilder(ctx)
	builder.
		With(nil, echo.Config{
			Service:   LatestSvc,
			Namespace: apps.LatestNs,
			Ports:     VersionedEchoPorts,
			Cluster:   c,
		}).
		With(nil, echo.Config{
			Service:   NMinusOneSvc,
			Namespace: apps.NMinusOneNs,
			Ports:     VersionedEchoPorts,
			Cluster:   c,
		}).
		With(nil, echo.Config{
			Service:   NMinusTwoSvc,
			Namespace: apps.NMinusTwoNs,
			Ports:     VersionedEchoPorts,
			Cluster:   c,
		})

	echos, err := builder.Build()
	if err != nil {
		return err
	}

	apps.All = echos
	apps.Latest = echos.Match(echo.Service(LatestSvc))
	apps.NMinusOne = echos.Match(echo.Service(NMinusOneSvc))
	apps.NMinusTwo = echos.Match(echo.Service(NMinusTwoSvc))

	return nil
}
