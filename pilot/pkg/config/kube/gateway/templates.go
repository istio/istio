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

package gateway

import (
	"embed"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"sigs.k8s.io/yaml"
)

// Templates embeds the templates
//go:embed templates/*
var Templates embed.FS

func funcMap() template.FuncMap {
	f := sprig.TxtFuncMap()
	// strdict is the same as the "dict" function (http://masterminds.github.io/sprig/dicts.html)
	// but returns a map[string]string instead of interface{} types. This allows it to be used
	// in annotations/labels.
	f["strdict"] = func(v ...string) map[string]string {
		dict := map[string]string{}
		lenv := len(v)
		for i := 0; i < lenv; i += 2 {
			key := v[i]
			if i+1 >= lenv {
				dict[key] = ""
				continue
			}
			dict[key] = v[i+1]
		}
		return dict
	}
	f["toYamlMap"] = func(mps ...map[string]string) string {
		data, err := yaml.Marshal(mergeMaps(mps...))
		if err != nil {
			return ""
		}
		return strings.TrimSuffix(string(data), "\n")
	}
	f["omit"] = func(dict map[string]string, keys ...string) map[string]string {
		res := map[string]string{}

		omit := make(map[string]bool, len(keys))
		for _, k := range keys {
			omit[k] = true
		}

		for k, v := range dict {
			if _, ok := omit[k]; !ok {
				res[k] = v
			}
		}
		return res
	}
	return f
}

func processTemplates() *template.Template {
	t, err := template.New("gateway").Funcs(funcMap()).ParseFS(Templates, "templates/*.yaml")
	if err != nil {
		panic(err.Error())
	}
	return t
}
