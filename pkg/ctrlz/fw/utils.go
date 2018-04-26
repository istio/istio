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

package fw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
)

// RenderError outputs an error message
func RenderError(w http.ResponseWriter, statusCode int, err error) {
	w.WriteHeader(statusCode)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "%v", err)
}

// RenderHTML executes the given template, sending the output to the supplied response writer
func RenderHTML(w http.ResponseWriter, t *template.Template, data interface{}) {
	b := &bytes.Buffer{}

	if err := t.Execute(b, data); err != nil {
		RenderError(w, http.StatusInternalServerError, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	b.WriteTo(w)
}

// RenderJSON outputs the given data as JSON
func RenderJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.WriteHeader(statusCode)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if err := json.NewEncoder(w).Encode(data); err != nil {
		RenderError(w, http.StatusInternalServerError, err)
	}
}
