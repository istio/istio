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

	"istio.io/pkg/ctrlz/fw"
	"istio.io/pkg/ctrlz/topics/assets"
)

type argsTopic struct {
}

// ArgsTopic returns a ControlZ topic that allows visualization of process command-line arguments.
func ArgsTopic() fw.Topic {
	return argsTopic{}
}

func (argsTopic) Title() string {
	return "Command-Line Arguments"
}

func (argsTopic) Prefix() string {
	return "arg"
}

func (argsTopic) Activate(context fw.TopicContext) {
	tmpl := template.Must(context.Layout().Parse(string(assets.MustAsset("templates/args.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fw.RenderHTML(w, tmpl, os.Args)
	})
}
