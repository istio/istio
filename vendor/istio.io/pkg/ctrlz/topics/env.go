// Copyright 2018 Istio Authors
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

package topics

import (
	"html/template"
	"net/http"
	"os"
	"sort"
	"strings"

	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/ctrlz/topics/assets"
)

type envTopic struct {
}

// EnvTopic returns a ControlZ topic that allows visualization of process environment variables.
func EnvTopic() fw.Topic {
	return envTopic{}
}

func (envTopic) Title() string {
	return "Environment Variables"
}

func (envTopic) Prefix() string {
	return "env"
}

type envVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func getVars() []envVar {
	env := os.Environ()
	sort.Strings(env)

	result := []envVar{}
	for _, v := range env {
		var eq = strings.Index(v, "=")
		var name = v[:eq]
		var value = v[eq+1:]
		result = append(result, envVar{Name: name, Value: value})
	}

	return result
}

func (envTopic) Activate(context fw.TopicContext) {
	tmpl := template.Must(context.Layout().Parse(string(assets.MustAsset("templates/env.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderHTML(w, tmpl, getVars())
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderJSON(w, http.StatusOK, getVars())
	})
}
