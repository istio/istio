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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
)

type fakeClientWrapper struct {
	client.Client
}

func FuzzHelmReconciler(data []byte) int {
	f := fuzz.NewConsumer(data)
	k8sobjBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	k8obj, err := object.ParseYAMLToK8sObject(k8sobjBytes)
	if err != nil {
		return 0
	}
	m := name.Manifest{}
	err = f.GenerateStruct(&m)
	if err != nil {
		return 0
	}
	obj := k8obj.UnstructuredObject()
	gvk := obj.GetObjectKind().GroupVersionKind()
	if len(gvk.Kind) == 0 {
		return 0
	}
	if len(gvk.Version) == 0 {
		return 0
	}
	cl := &fakeClientWrapper{fake.NewClientBuilder().WithRuntimeObjects(obj).Build()}
	h, err := helmreconciler.NewHelmReconciler(cl, nil, nil, nil)
	if err != nil {
		return 0
	}
	_ = h.ApplyObject(obj, false)
	_, _ = h.Reconcile()
	_, _, _ = h.ApplyManifest(m, false)
	return 1
}
