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

package fuzz

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"
	corev1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
)

func FuzzConvertIngressVirtualService(data []byte) int {
	f := fuzz.NewConsumer(data)
	ing := knetworking.Ingress{}
	err := f.GenerateStruct(&ing)
	if err != nil {
		return 0
	}
	service := &corev1.Service{}
	cfgs := map[string]*config.Config{}
	serviceLister, teardown := newServiceLister(service)
	defer teardown()
	ingress.ConvertIngressVirtualService(ing, "mydomain", cfgs, serviceLister)
	return 1
}

func FuzzConvertIngressV1alpha3(data []byte) int {
	f := fuzz.NewConsumer(data)
	ing := knetworking.Ingress{}
	err := f.GenerateStruct(&ing)
	if err != nil {
		return 0
	}
	m := &meshconfig.MeshConfig{}
	err = f.GenerateStruct(m)
	if err != nil {
		return 0
	}
	ingress.ConvertIngressV1alpha3(ing, m, "mydomain")
	return 1
}

func newServiceLister(objects ...runtime.Object) (kclient.Client[*corev1.Service], func()) {
	kc := kube.NewFakeClient(objects...)
	stop := make(chan struct{})
	kc.RunAndWait(stop)
	teardown := func() {
		close(stop)
	}
	return kclient.New[*corev1.Service](kc), teardown
}
