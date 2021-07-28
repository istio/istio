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
	"os"
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pkg/test"
)

func init() {
	testing.Init()
}

type NopTester struct{}

func (n NopTester) Fail() {}

func (n NopTester) FailNow() {}

func (n NopTester) Fatal(args ...interface{}) {}

func (n NopTester) Fatalf(format string, args ...interface{}) {}

func (n NopTester) Log(args ...interface{}) {}

func (n NopTester) Logf(format string, args ...interface{}) {}

func (n NopTester) TempDir() string {
	tempDir, _ := os.MkdirTemp("", "test")
	return tempDir
}

func (n NopTester) Helper() {}

func (n NopTester) Cleanup(f func()) {}

var _ test.Failer = NopTester{}

func FuzzValidateClusters(data []byte) int {
	proxy := model.Proxy{}
	f := fuzz.NewConsumer(data)
	err := f.GenerateStruct(&proxy)
	if err != nil {
		return 0
	}
	to := v1alpha3.TestOptions{}
	err = f.GenerateStruct(&to)
	if err != nil {
		return 0
	}
	cg := v1alpha3.NewConfigGenTest(NopTester{}, to)
	p := cg.SetupProxy(&proxy)
	_ = cg.Clusters(p)
	_ = cg.Routes(p)
	return 1
}
