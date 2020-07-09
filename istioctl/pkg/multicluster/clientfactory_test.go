// Copyright Istio Authors.
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/kube"
	testKube "istio.io/istio/pkg/test/kube"
)

func newFakeKubeClient(server string, objs ...runtime.Object) testKube.MockClient {
	return testKube.MockClient{
		Interface: fake.NewSimpleClientset(objs...),
		ConfigValue: &rest.Config{
			Host: server,
		},
	}
}

func fakeClientset(client kube.Client) *fake.Clientset {
	return client.Kube().(*fake.Clientset)
}
