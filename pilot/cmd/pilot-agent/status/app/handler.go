// Copyright 2019 Istio Authors
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

package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"istio.io/istio/pkg/log"
)

// RegisterHTTPHandlerFunc registers the HTTP handler function for the app probe.
func RegisterHTTPHandlerFunc(handler *http.ServeMux, probeMap ProbeMap) {
	handlerFn := func(w http.ResponseWriter, req *http.Request) {
		// Validate the request first.
		path := req.URL.Path
		if !strings.HasPrefix(path, "/") {
			path = "/" + req.URL.Path
		}
		prober, exists := probeMap[ProbeURLPath(path)]
		if !exists {
			log.Errorf("Prober does not exists url %v", path)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(fmt.Sprintf("app prober config does not exists for %v", path)))
			return
		}

		// Construct a request sent to the application.
		httpClient := &http.Client{
			// TODO: figure out the appropriate timeout?
			Timeout: 10 * time.Second,
		}
		url := fmt.Sprintf("http://127.0.0.1:%v%s", prober.Port.IntValue(), prober.Path)
		appReq, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Errorf("Failed to create request to probe app %v, original url %v", err, path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		for _, header := range prober.HTTPHeaders {
			appReq.Header[header.Name] = []string{header.Value}
		}

		// Send the request.
		response, err := httpClient.Do(appReq)
		if err != nil {
			log.Errorf("Request to probe app failed: %v, original URL path = %v\napp URL path = %v", err, path, prober.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer func() { _ = response.Body.Close() }()

		// We only write the status code to the response.
		w.WriteHeader(response.StatusCode)
	}

	// Register the probe handler function.
	handler.HandleFunc("/", handlerFn)
	handler.HandleFunc("/app-health", handlerFn)
}
