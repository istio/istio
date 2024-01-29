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

package bootstrap

import (
	"net/http"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
)

func InitGenerators(
	s *xds.DiscoveryServer,
	cg core.ConfigGenerator,
	systemNameSpace string,
	clusterID cluster.ID,
	internalDebugMux *http.ServeMux,
) {
	env := s.Env
	generators := map[string]model.XdsResourceGenerator{}
	edsGen := &xds.EdsGenerator{Cache: s.Cache, EndpointIndex: env.EndpointIndex}
	generators[v3.ClusterType] = &xds.CdsGenerator{ConfigGenerator: cg}
	generators[v3.ListenerType] = &xds.LdsGenerator{ConfigGenerator: cg}
	generators[v3.RouteType] = &xds.RdsGenerator{ConfigGenerator: cg}
	generators[v3.EndpointType] = edsGen
	ecdsGen := &xds.EcdsGenerator{ConfigGenerator: cg}
	if env.CredentialsController != nil {
		generators[v3.SecretType] = xds.NewSecretGen(env.CredentialsController, s.Cache, clusterID, env.Mesh())
		ecdsGen.SetCredController(env.CredentialsController)
	}
	generators[v3.ExtensionConfigurationType] = ecdsGen
	generators[v3.NameTableType] = &xds.NdsGenerator{ConfigGenerator: cg}
	generators[v3.ProxyConfigType] = &xds.PcdsGenerator{TrustBundle: env.TrustBundle}

	workloadGen := &xds.WorkloadGenerator{Server: s}
	generators[v3.AddressType] = workloadGen
	generators[v3.WorkloadType] = workloadGen
	generators[v3.WorkloadAuthorizationType] = &xds.WorkloadRBACGenerator{Server: s}

	generators["grpc"] = &grpcgen.GrpcConfigGenerator{}
	generators["grpc/"+v3.EndpointType] = edsGen
	generators["grpc/"+v3.ListenerType] = generators["grpc"]
	generators["grpc/"+v3.RouteType] = generators["grpc"]
	generators["grpc/"+v3.ClusterType] = generators["grpc"]

	generators["api"] = apigen.NewGenerator(env.ConfigStore)
	generators["api/"+v3.EndpointType] = edsGen

	generators["event"] = xds.NewStatusGen(s)
	generators[v3.DebugType] = xds.NewDebugGen(s, systemNameSpace, internalDebugMux)
	s.Generators = generators
}
