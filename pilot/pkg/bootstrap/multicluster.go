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
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"

	"istio.io/pkg/log"
)

// initClusterRegistries starts the secret controller to watch for remote
// clusters and initialize the multicluster structures.
func (s *Server) initClusterRegistries(args *PilotArgs) (err error) {
	if hasKubeRegistry(args.RegistryOptions.Registries) {
		log.Info("initializing Kubernetes cluster registry")
		mc, err := controller.NewMulticluster(s.kubeClient,
			args.RegistryOptions.ClusterRegistriesNamespace,
			args.RegistryOptions.KubeOptions,
			s.ServiceController(),
			s.EnvoyXdsServer,
			s.environment)

		if err != nil {
			log.Info("Unable to create new Multicluster object")
			return err
		}

		s.multicluster = mc
	}
	return nil
}
