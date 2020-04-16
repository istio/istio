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

package httprequest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet(t *testing.T) {
	expected := "fooey-baroque"
	url := "/fooey/baroque/cazoo"
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.String() != url {
			t.Errorf("Made request to wrong URL, want: %s, got: %s", url, req.URL.String())
		}
		rw.Write([]byte(expected))
	}))
	defer server.Close()
	response, err := Get(server.URL + url)
	if err != nil {
		t.Errorf("Unexpected Error In Making Request: %s", err.Error())
	}
	if expected != string(response) {
		t.Errorf("Returned unexpected response, want: %s, got: %s", expected, string(response))
	}
}
