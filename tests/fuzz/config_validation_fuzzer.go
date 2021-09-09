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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube"
)

func FuzzConfigValidation(data []byte) int {
	f := fuzz.NewConsumer(data)
	var iobj *config.Config
	configIndex, err := f.GetInt()
	if err != nil {
		return -1
	}

	r := collections.Pilot.All()[configIndex%len(collections.Pilot.All())]
	gvk := r.Resource().GroupVersionKind()
	kgvk := schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
	object, err := kube.IstioScheme.New(kgvk)
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
	_, _ = r.Resource().ValidateConfig(*iobj)
	return 1
}

// FuzzConfigValidation2 implements a second fuzzer for config validation.
// The fuzzer targets the same API as FuzzConfigValidation above,
// but its approach to creating a fuzzed config is a bit different
// in that it utilizes Istio APIs to generate a Spec from json.
// We currently run both continuously to compare their performance.
func FuzzConfigValidation2(data []byte) int {
	f := fuzz.NewConsumer(data)
	configIndex, err := f.GetInt()
	if err != nil {
		return -1
	}
	r := collections.Pilot.All()[configIndex%len(collections.Pilot.All())]

	spec, err := r.Resource().NewInstance()
	if err != nil {
		return 0
	}
	jsonData, err := f.GetString()
	if err != nil {
		return 0
	}
	err = config.ApplyJSON(spec, jsonData)
	if err != nil {
		return 0
	}

	m := config.Meta{}
	err = f.GenerateStruct(&m)
	if err != nil {
		return 0
	}

	gvk := r.Resource().GroupVersionKind()
	m.GroupVersionKind = gvk

	_, _ = r.Resource().ValidateConfig(config.Config{
		Meta: m,
		Spec: spec,
	})
	return 1
}
