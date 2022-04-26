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
	"errors"
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/tests/fuzz/utils"
)

func init() {
	testing.Init()
}

func ValidateTestOptions(to v1alpha3.TestOptions) error {
	for _, csc := range to.ConfigStoreCaches {
		if csc == nil {
			return errors.New("a ConfigStoreController was nil")
		}
	}
	for _, sr := range to.ServiceRegistries {
		if sr == nil {
			return errors.New("a ServiceRegistry was nil")
		}
	}
	return nil
}

func FuzzValidateClusters(data []byte) int {
	proxy := model.Proxy{}
	f := fuzz.NewConsumer(data)
	to := v1alpha3.TestOptions{}
	err := f.GenerateStruct(&to)
	if err != nil {
		return 0
	}
	err = ValidateTestOptions(to)
	if err != nil {
		return 0
	}
	err = f.GenerateStruct(&proxy)
	if err != nil {
		return 0
	}
	cg := v1alpha3.NewConfigGenTest(utils.NopTester{}, to)
	p := cg.SetupProxy(&proxy)
	_ = cg.Clusters(p)
	_ = cg.Routes(p)
	return 1
}
