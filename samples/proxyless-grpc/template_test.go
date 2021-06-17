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

package proxyless_grpc_samples_test

import (
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"gopkg.in/yaml.v3"

	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/env"
)

// TestTemplates ensures the files in this dir have the expected layout and are valid Go templates
func TestTemplates(t *testing.T) {
	path := filepath.Join(env.IstioSrc, "samples/proxyless-grpc")
	files, err := ioutil.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".yaml") {
			continue
		}
		t.Run(file.Name(), func(t *testing.T) {
			data, err := ioutil.ReadFile(filepath.Join(path, file.Name()))
			if err != nil {
				t.Fatal(err)
			}
			m := map[string]string{}
			if err := yaml.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			for tmplName, tmplText := range m {
				t.Run(tmplName, func(t *testing.T) {
					tmpl := template.New(tmplName)
					_, err := tmpl.
						Funcs(sprig.TxtFuncMap()).
						Funcs(inject.CreateInjectionFuncmap()).
						Parse(tmplText)
					if err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
