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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/operator/pkg/util/httpserver"
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
	}
	tmp, err := ioutil.TempDir("", InstallationDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	server := httpserver.NewServer(tmp)
	defer server.Close()
	if _, err := server.MoveFiles("testdata/*.tar.gz*"); err != nil {
		t.Error(err)
		return
	}
	for _, tt := range tests {
		outdir := filepath.Join(server.Root, "testout")
		os.RemoveAll(outdir)
		os.Mkdir(outdir, 0755)
		fq := NewURLFetcher(server.URL()+"/"+tt.installationPackageName, tmp+"/testout")

		err = fq.Fetch()
		if err != nil {
			t.Error(err)
			return
		}

		ef := filepath.Join(fq.destDirRoot, tt.installationPackageName)
		if _, err := os.Stat(ef); err != nil {
			t.Error(err)
			return
		}
	}
}
