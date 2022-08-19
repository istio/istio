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
	kubemanagement "istio.io/istio/pilot/pkg/management/kube"
)

func (s *Server) initManagementController(args *PilotArgs) error {
	if s.multiclusterController != nil {
		s.multiclusterController.AddHandler(kubemanagement.NewMulticluster(args.PodName,
			kubemanagement.Options{
				SystemNamespace: args.Namespace,
				ClusterID:       s.clusterID,
			},
			s.istiodCertBundleWatcher,
			args.Revision,
			s.shouldStartNsController(),
			s.server))
	}
	return nil
}
