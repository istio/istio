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
	fuzz "github.com/AdaLogics/go-fuzz-headers"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	clienttelemetry "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
)

var scheme = runtime.NewScheme()

func init() {
	clientnetworkingalpha.AddToScheme(scheme)
	clientnetworkingbeta.AddToScheme(scheme)
	clientsecurity.AddToScheme(scheme)
	clienttelemetry.AddToScheme(scheme)
}

func FuzzConfigValidation(data []byte) int {
	f := fuzz.NewConsumer(data)
	var iobj *config.Config
	configIndex, err := f.GetInt()
	if err != nil {
		return 0
	}

	r := collections.Pilot.All()[configIndex%len(collections.Pilot.All())]
	gvk := r.Resource().GroupVersionKind()
	kgvk := schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
	object, err := scheme.New(kgvk)
	if err != nil {
		return 0
	}
	_, err = apimeta.TypeAccessor(object)
	if err != nil {
		return 0
	}

	err = f.GenerateStruct(&object)
	if err != nil {
		return 0
	}

	iobj = crdclient.TranslateObject(object, gvk, "cluster.local")
	_, err = r.Resource().ValidateConfig(*iobj)
	if err != nil {
		return 0
	}
	return 1
}
