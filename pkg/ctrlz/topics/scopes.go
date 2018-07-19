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
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"

	"istio.io/istio/pkg/ctrlz/fw"
	"istio.io/istio/pkg/log"
)

type scopeTopic struct {
}

type scopeInfo struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	OutputLevel     string `json:"output_level"`
	StackTraceLevel string `json:"stack_trace_level"`
	LogCallers      bool   `json:"log_callers"`
}

var levelToString = map[log.Level]string{
	log.DebugLevel: "debug",
	log.InfoLevel:  "info",
	log.WarnLevel:  "warn",
	log.ErrorLevel: "error",
	log.NoneLevel:  "none",
}

var stringToLevel = map[string]log.Level{
	"debug": log.DebugLevel,
	"info":  log.InfoLevel,
	"warn":  log.WarnLevel,
	"error": log.ErrorLevel,
	"none":  log.NoneLevel,
}

// ScopeTopic returns a ControlZ topic that allows visualization of process logging scopes.
func ScopeTopic() fw.Topic {
	return scopeTopic{}
}

func (scopeTopic) Title() string {
	return "Logging Scopes"
}

func (scopeTopic) Prefix() string {
	return "scope"
}

func getScopeInfo(s *log.Scope) *scopeInfo {
	return &scopeInfo{
		Name:            s.Name(),
		Description:     s.Description(),
		OutputLevel:     levelToString[s.GetOutputLevel()],
		StackTraceLevel: levelToString[s.GetStackTraceLevel()],
		LogCallers:      s.GetLogCallers(),
	}
}

func (scopeTopic) Activate(context fw.TopicContext) {
	tmpl := template.Must(context.Layout().Parse(string(MustAsset("assets/templates/scopes.html"))))

	_ = context.HTMLRouter().NewRoute().HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		allScopes := log.Scopes()
		s := make([]scopeInfo, 0, len(allScopes))
		for _, scope := range allScopes {
			s = append(s, *getScopeInfo(scope))
		}
		fw.RenderHTML(w, tmpl, s)
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("GET").Path("/").HandlerFunc(getAllScopes)
	_ = context.JSONRouter().NewRoute().Methods("GET").Path("/{scope}").HandlerFunc(getScope)
	_ = context.JSONRouter().NewRoute().Methods("PUT").Path("/{scope}").HandlerFunc(putScope)
}

func getAllScopes(w http.ResponseWriter, req *http.Request) {
	allScopes := log.Scopes()

	scopeInfos := make([]scopeInfo, 0, len(allScopes))
	for _, s := range allScopes {
		scopeInfos = append(scopeInfos, *getScopeInfo(s))
	}

	fw.RenderJSON(w, http.StatusOK, scopeInfos)
}

func getScope(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["scope"]

	if s := log.FindScope(name); s != nil {
		fw.RenderJSON(w, http.StatusOK, getScopeInfo(s))
		return
	}

	fw.RenderError(w, http.StatusBadRequest, fmt.Errorf("unknown scope name: %s", name))
}

func putScope(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["scope"]

	var info scopeInfo
	if err := json.NewDecoder(req.Body).Decode(&info); err != nil {
		fw.RenderError(w, http.StatusBadRequest, fmt.Errorf("unable to decode request: %v", err))
		return
	}

	if s := log.FindScope(name); s != nil {
		level, ok := stringToLevel[info.OutputLevel]
		if ok {
			s.SetOutputLevel(level)
		}

		level, ok = stringToLevel[info.StackTraceLevel]
		if ok {
			s.SetStackTraceLevel(level)
		}

		s.SetLogCallers(info.LogCallers)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	fw.RenderError(w, http.StatusBadRequest, fmt.Errorf("unknown scope name: %s", name))
}
