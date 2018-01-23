// Copyright 2017 Istio Authors
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

package list

import (
	adapter2 "istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template"
	"os"
	"testing"
)

var yml string = `
{
  "configs": [
    "../../testdata/config/listcheck.yaml",
    "../../testdata/config/attributes.yaml"
  ],
  "attributes": {
    "destination.labels": {
      "app": "ratings"
    },
    "source.labels": {
      "version": "v1"
    }
  },
  "baseline": "baseline.txt"
}
`

func TestReport(t *testing.T) {
	wd, _ := os.Getwd()
	test.Test2(
		t,
		wd,
		[]adapter2.InfoFn{GetInfo},
		template.SupportedTmplInfo,
		yml,)
}
