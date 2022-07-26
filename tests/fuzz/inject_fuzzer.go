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
	"bytes"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/inject"
)

func FuzzIntoResourceFile(data []byte) int {
	f := fuzz.NewConsumer(data)
	var sidecarTemplate map[string]string
	err := f.FuzzMap(&sidecarTemplate)
	if err != nil {
		return 0
	}
	valuesConfig, err := f.GetString()
	if err != nil {
		return 0
	}
	meshYaml, err := f.GetString()
	if err != nil {
		return 0
	}
	mc, err := mesh.ApplyMeshConfigDefaults(meshYaml)
	if err != nil {
		return 0
	}
	inData, err := f.GetBytes()
	if err != nil {
		return 0
	}
	in := bytes.NewReader(inData)
	var got bytes.Buffer
	warn := func(s string) {}
	revision, err := f.GetString()
	if err != nil {
		return 0
	}
	templs, err := inject.ParseTemplates(sidecarTemplate)
	if err != nil {
		return 0
	}
	vc, err := inject.NewValuesConfig(valuesConfig)
	if err != nil {
		return 0
	}
	_ = inject.IntoResourceFile(nil, templs, vc, revision, mc, in, &got, warn)
	return 1
}
