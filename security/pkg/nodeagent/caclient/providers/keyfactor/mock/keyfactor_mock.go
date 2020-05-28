// Copyright 2020 Istio Authors
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

package caclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
)

// CAServer is the mocked Mesh CA server.
type CAServer struct {
	Server  *httptest.Server
	Address string
}

// CreateServer creates a mocked local KeyfactorCA server and runs it in a separate thread.
// nolint: interfacer
func CreateServer(responseError bool, responseData interface{}, requestBodyChan chan map[string]interface{}) *CAServer {
	// create a local https server
	s := &CAServer{}

	handler := http.NewServeMux()
	handler.HandleFunc("/KeyfactorAPI/Enrollment/CSR", func(w http.ResponseWriter, r *http.Request) {

		if requestBodyChan != nil {
			var requestBody map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&requestBody)
			if err != nil {
				fmt.Println("got error on Mock KeyfactorCa: requestBodyChan")
			}
			requestBodyChan <- requestBody
		}

		if responseError {
			w.WriteHeader(http.StatusInternalServerError)
			_, err := w.Write([]byte("500 - Something bad happened!"))
			if err != nil {
				fmt.Println("got error on Mock KeyfactorCa: responseError")
			}
			return
		}

		j, _ := json.Marshal(responseData)
		_, _ = w.Write(j)
	})

	s.Server = httptest.NewServer(handler)
	s.Address = s.Server.URL
	return s
}
