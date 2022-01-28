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

package helm

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetch(t *testing.T) {
	tests := []struct {
		name                    string
		installationPackageName string
	}{
		{
			name:                    "Charts download only",
			installationPackageName: "istio-1.3.0-linux.tar.gz",
		},
		{
			name:                    "Charts download only in folders",
			installationPackageName: "testdata/istio-1.3.0-linux.tar.gz",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".tar.gz") {
			http.NotFound(rw, r)
			return
		}
		http.ServeFile(rw, r, "testdata/istio-1.3.0-linux.tar.gz")
	}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			rootDir := tmp + "/testout"
			fq := NewURLFetcher(server.URL+"/"+tt.installationPackageName, rootDir)

			err := fq.Fetch()
			if err != nil {
				t.Error(err)
				return
			}

			ef := filepath.Join(rootDir, filepath.Base(tt.installationPackageName))
			if _, err := os.Stat(ef); err != nil {
				t.Error(err)
				return
			}
		})
	}
}
