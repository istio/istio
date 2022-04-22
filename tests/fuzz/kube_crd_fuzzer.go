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

	"istio.io/istio/pilot/pkg/config/kube/crd"
	config2 "istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
)

// FuzzKubeCRD implements a fuzzer that targets
// the kube CRD in two steps.
// It first creates an object with a config
// that has had pseudo-random values inserted.
// When a valid object has been created, it
// tries and convert that object. If this
// conversion fails, it panics.
func FuzzKubeCRD(data []byte) int {
	f := fuzz.NewConsumer(data)
	config := config2.Config{}
	err := f.GenerateStruct(&config)
	if err != nil {
		return 0
	}

	// Create a valid obj:
	obj, err := crd.ConvertConfig(config)
	if err != nil {
		return 0
	}

	// Convert the obj and report if it fails.
	_, err = crd.ConvertObject(collections.IstioNetworkingV1Alpha3Virtualservices, obj, "cluster")
	if err != nil {
		panic(err)
	}
	return 1
}
