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

package mock

import (
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/rest"
)

type appsv1Impl struct {
	apps appsv1.DeploymentInterface
}

var _ appsv1.AppsV1Interface = &appsv1Impl{}

func (c *appsv1Impl) ControllerRevisions(namespace string) appsv1.ControllerRevisionInterface {
	panic("not implemented")
}

func (c *appsv1Impl) DaemonSets(namespace string) appsv1.DaemonSetInterface {
	panic("not implemented")
}

func (c *appsv1Impl) Deployments(namespace string) appsv1.DeploymentInterface {
	return c.apps
}

func (c *appsv1Impl) RESTClient() rest.Interface {
	panic("not implemented")
}

func (c *appsv1Impl) ReplicaSets(namespace string) appsv1.ReplicaSetInterface {
	panic("not implemented")
}

func (c *appsv1Impl) StatefulSets(namespace string) appsv1.StatefulSetInterface {
	panic("not implemented")
}
