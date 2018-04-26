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

// Package topics defines several canonical ControlZ topics.
package topics

import (
	"html/template"
	"net/http"
	"runtime"

	"istio.io/istio/pkg/ctrlz/fw"
)

type memTopic struct {
}

// MemTopic returns a ControlZ topic that allows visualization of process memory usage.
func MemTopic() fw.Topic {
	return memTopic{}
}

func (memTopic) Title() string {
	return "Memory Usage"
}

func (memTopic) Prefix() string {
	return "mem"
}

func (memTopic) Activate(context fw.TopicContext) {
	tmpl := template.Must(context.Layout().Parse(string(MustAsset("assets/templates/mem.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ms := &runtime.MemStats{}
		runtime.ReadMemStats(ms)
		fw.RenderHTML(w, tmpl, ms)
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ms := &runtime.MemStats{}
		runtime.ReadMemStats(ms)
		fw.RenderJSON(w, http.StatusOK, ms)
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("PUT").Path("/forcecollection").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		runtime.GC()
		w.WriteHeader(http.StatusAccepted)
	})
}
