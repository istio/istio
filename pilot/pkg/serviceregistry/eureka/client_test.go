// Copyright 2017 Istio Authors
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

package eureka

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

func readFile(t *testing.T, filename string) []byte {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Error(err)
	}
	return data
}

func TestClient(t *testing.T) {
	clientTests := []struct {
		context    string
		data       []byte
		statusCode int
		apps       []*application
		shouldErr  bool
	}{
		{
			context:    "no applications",
			data:       readFile(t, "testdata/eureka-no-apps.json"),
			statusCode: http.StatusOK,
			apps:       make([]*application, 0),
		},
		{
			context: "multiple applications",
			data:    readFile(t, "testdata/eureka-apps.json"),
			apps: []*application{
				{
					Name: appName("foo.bar.local"),
					Instances: []*instance{
						makeInstance("foo.bar.local", "10.0.0.1", 5000, 5443,
							metadata{protocolMetadata: "HTTP"}),
						makeInstance("foo.bar.local", "10.0.0.2", 6000, -1,
							metadata{protocolMetadata: "HTTP"}),
					},
				},
				{
					Name: appName("foo.biz.local"),
					Instances: []*instance{
						makeInstance("foo.biz.local", "10.0.0.3", 8080, -1,
							metadata{protocolMetadata: "HTTP2"}),
					},
				},
			},
			statusCode: http.StatusOK,
		},
		{
			context:    "non-200 response",
			statusCode: http.StatusNotFound,
			shouldErr:  true,
		},
	}

	for _, tt := range clientTests {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.statusCode)
			w.Write(tt.data) // nolint: errcheck
		}))
		cl := NewClient(ts.URL)

		apps, err := cl.Applications()
		if !tt.shouldErr && err != nil {
			t.Errorf("unexpected error retrieving Eureka applications for %s context: %v", tt.context, err)
		} else if tt.shouldErr && err == nil {
			t.Errorf("expected error, got nil when retrieving Eureka applications for %s context", tt.context)
		}

		if err := compare(t, apps, tt.apps); err != nil {
			t.Errorf("retrieved Eureka applications do not match expected for %s context:\n%v", tt.context, err)
		}

		ts.Close()
	}
}
