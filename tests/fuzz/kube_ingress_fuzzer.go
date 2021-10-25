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
	"time"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	coreV1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	networkingV1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	kubeIngress "istio.io/istio/pilot/pkg/config/kube/ingress"
	ingressv1 "istio.io/istio/pilot/pkg/config/kube/ingressv1"
	"istio.io/istio/pkg/config"
)

func FuzzConvertIngressVirtualService(data []byte) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f := fuzz.NewConsumer(data)
	ingress := knetworking.Ingress{}
	err := f.GenerateStruct(&ingress)
	if err != nil {
		return 0
	}
	service := &coreV1.Service{}
	cfgs := map[string]*config.Config{}
	serviceLister := createFakeLister(ctx, service)
	ingressv1.ConvertIngressVirtualService(ingress, "mydomain", cfgs, serviceLister)
	return 1
}

func FuzzConvertIngressVirtualService2(data []byte) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f := fuzz.NewConsumer(data)
	ingress := networkingV1beta1.Ingress{}
	err := f.GenerateStruct(&ingress)
	if err != nil {
		return 0
	}
	service := &coreV1.Service{}
	cfgs := map[string]*config.Config{}
	serviceLister := createFakeLister(ctx, service)
	kubeIngress.ConvertIngressVirtualService(ingress, "mydomain", cfgs, serviceLister)
	return 1
}

func FuzzConvertIngressV1alpha3(data []byte) int {
	f := fuzz.NewConsumer(data)
	ingress := knetworking.Ingress{}
	err := f.GenerateStruct(&ingress)
	if err != nil {
		return 0
	}
	m := &meshconfig.MeshConfig{}
	err = f.GenerateStruct(m)
	if err != nil {
		return 0
	}
	ingressv1.ConvertIngressV1alpha3(ingress, m, "mydomain")
	return 1
}

func FuzzConvertIngressV1alpha32(data []byte) int {
	f := fuzz.NewConsumer(data)
	ingress := networkingV1beta1.Ingress{}
	err := f.GenerateStruct(&ingress)
	if err != nil {
		return 0
	}
	m := &meshconfig.MeshConfig{}
	err = f.GenerateStruct(m)
	if err != nil {
		return 0
	}
	kubeIngress.ConvertIngressV1alpha3(ingress, m, "mydomain")
	return 1
}

func createFakeLister(ctx context.Context, objects ...runtime.Object) listerv1.ServiceLister {
	client := fake.NewSimpleClientset(objects...)
	informerFactory := informers.NewSharedInformerFactory(client, time.Hour)
	svcInformer := informerFactory.Core().V1().Services().Informer()
	go svcInformer.Run(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), svcInformer.HasSynced)
	return informerFactory.Core().V1().Services().Lister()
}
