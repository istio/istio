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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	ktypes "istio.io/istio/pkg/kube/kubetypes"
)

func TestCustomRegistration(t *testing.T) {
	Register[*v1.NetworkPolicy](NewTypeRegistration[*v1.NetworkPolicy](
		schema.GroupVersionResource{},
		config.GroupVersionKind{},
		&v1.NetworkPolicy{},
		func(c ClientGetter, o ktypes.InformerOptions) cache.ListerWatcher {
			np := c.Kube().NetworkingV1().NetworkPolicies(o.Namespace)
			return &cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					options.FieldSelector = o.FieldSelector
					options.LabelSelector = o.LabelSelector
					return np.List(context.Background(), options)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					options.FieldSelector = o.FieldSelector
					options.LabelSelector = o.LabelSelector
					return np.Watch(context.Background(), options)
				},
				DisableChunking: true,
			}
		},
	))

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "funkyns"}}

	client := kube.NewFakeClient(ns)

	inf := GetInformerFiltered[*v1.NetworkPolicy](client, ktypes.InformerOptions{})
	if inf.Informer == nil {
		t.Errorf("Expected valid informer, got empty")
	}
}

var _ TypeRegistration[*v1.NetworkPolicy] = (*internalTypeReg[*v1.NetworkPolicy])(nil)
