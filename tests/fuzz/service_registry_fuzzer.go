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

// nolint: golint
package fuzz

import (
	"context"
	"fmt"
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	discovery "k8s.io/api/discovery/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/pkg/xds"
)

func init() {
	testing.Init()
}

func createEP(f *fuzz.ConsumeFuzzer) (*discovery.EndpointSlice, error) {
	endpointSlice := &discovery.EndpointSlice{}
	err := f.GenerateStruct(endpointSlice)
	if err != nil {
		return endpointSlice, err
	}
	return endpointSlice, nil
}

func createEndpointSlice(endpointSlice *discovery.EndpointSlice, c kubernetes.Interface, namespace string) error {
	if _, err := c.DiscoveryV1beta1().EndpointSlices(namespace).Create(context.TODO(), endpointSlice, metav1.CreateOptions{}); err != nil {
		if kerrors.IsAlreadyExists(err) {
			_, err = c.DiscoveryV1beta1().EndpointSlices(namespace).Update(context.TODO(), endpointSlice, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func FuzzEndpointSlices(data []byte) int {
	t := &testing.T{}
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesEndpointMode: kubecontroller.EndpointSliceOnly,
	})
	f := fuzz.NewConsumer(data)

	iters, err := f.GetInt()
	if err != nil {
		return 0
	}
	maxIters := iters % 30
	for i := 0; i < maxIters; i++ {
		fmt.Println("iter ", i, " of ", maxIters)
		name, err := f.GetString()
		if err != nil {
			return 0
		}
		namespace, err := f.GetString()
		if err != nil {
			return 0
		}
		eps, err := createEP(f)
		if err != nil {
			return 0
		}

		eps.ObjectMeta.Name = name
		eps.ObjectMeta.Namespace = namespace

		fuzzBool, err := f.GetBool()
		if err != nil {
			return 0
		}
		if fuzzBool {
			_ = createEndpointSlice(eps, s.KubeClient(), namespace)
		} else {
			s.KubeClient().DiscoveryV1beta1().EndpointSlices(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
		}
	}
	return 1
}
