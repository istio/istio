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

package kubeclient

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"istio.io/istio/pkg/kube"
	ktypes "istio.io/istio/pkg/kube/kubetypes"
)

func TestCustomRegistration(t *testing.T) {
	gvr := v1.SchemeGroupVersion.WithResource("networkpolicies")
	gvk := v1.SchemeGroupVersion.WithKind("NetworkPolicy")
	Register[*v1.NetworkPolicy](
		gvr, gvk,
		func(c ClientGetter, namespace string, o metav1.ListOptions) (runtime.Object, error) {
			return c.Kube().NetworkingV1().NetworkPolicies(namespace).List(context.Background(), o)
		},
		func(c ClientGetter, namespace string, o metav1.ListOptions) (watch.Interface, error) {
			return c.Kube().NetworkingV1().NetworkPolicies(namespace).Watch(context.Background(), o)
		},
		func(c ClientGetter, namespace string) ktypes.WriteAPI[*v1.NetworkPolicy] {
			return c.Kube().NetworkingV1().NetworkPolicies(namespace)
		},
	)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "funkyns"}}

	client := kube.NewFakeClient(ns)

	inf := GetInformerFiltered[*v1.NetworkPolicy](client, ktypes.InformerOptions{}, gvr)
	if inf.Informer == nil {
		t.Errorf("Expected valid informer, got empty")
	}
}

var _ TypeRegistration[*v1.NetworkPolicy] = (*internalTypeReg[*v1.NetworkPolicy])(nil)
