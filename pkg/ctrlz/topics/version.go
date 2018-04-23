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

	"istio.io/istio/pkg/ctrlz/fw"
	"istio.io/istio/pkg/version"
)

type versionTopic struct {
}

// VersionTopic returns a ControlZ topic that allows visualization of versioning info.
func VersionTopic() fw.Topic {
	return versionTopic{}
}

func (versionTopic) Title() string {
	return "Version Info"
}

func (versionTopic) Prefix() string {
	return "version"
}

func (versionTopic) Activate(context fw.TopicContext) {
	tmpl := template.Must(context.Layout().Parse(string(MustAsset("assets/templates/version.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderHTML(w, tmpl, &version.Info)
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderJSON(w, http.StatusOK, &version.Info)
	})
}
