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

package ambient

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	securityclient "istio.io/client-go/pkg/apis/security/v1"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
)

const ClusterKRTMetadataKey = "cluster"

func (a *index) buildGlobalCollections(
	_ *Cluster,
	_ krt.Collection[*securityclient.AuthorizationPolicy],
	_ krt.Collection[*securityclient.PeerAuthentication],
	_ krt.Collection[*v1beta1.GatewayClass],
	_ krt.Collection[*networkingclient.WorkloadEntry],
	_ krt.Collection[*networkingclient.ServiceEntry],
	_ kclient.Informer[*networkingclient.ServiceEntry],
	_ kclient.Informer[*v1.Service],
	_ kclient.Informer[*securityclient.AuthorizationPolicy],
	options Options,
	opts krt.OptionsBuilder,
	configOverrides ...func(*rest.Config),
) {
	clusters := a.buildRemoteClustersCollection(
		options,
		opts,
		configOverrides...,
	)

	a.remoteClusters = clusters
	// TODO: build all remote collections and assign them to a
}
