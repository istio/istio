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

package httprequest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet(t *testing.T) {
	tests := []struct {
		desc          string
		expectedData  string
		expectedRoute string
	}{
		{
			desc:          "test-get",
			expectedData:  "fooey-baroque",
			expectedRoute: "/fooey/baroque/cazoo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			testServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.String() != tt.expectedRoute {
					t.Errorf("%s: request made to wrong URL, got %s, want %s", tt.desc, req.URL.String(), tt.expectedRoute)
				}
				rw.Write([]byte(tt.expectedData))
			}))
			defer testServer.Close()
			response, err := Get(testServer.URL + tt.expectedRoute)
			if err != nil {
				t.Errorf("Unexpected Error In Making Request: %s", err.Error())
			}
			if tt.expectedData != string(response) {
				t.Errorf("Returned unexpected response, want: %s, got: %s", tt.expectedData, string(response))
			}
		})
	}
}
