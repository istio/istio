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

package env

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	testenv "istio.io/istio/pkg/test/env"
)

// Date: 2/22/2023
const SHA = "359dcd3a19f109c50e97517fe6b1e2676e870c4d"

var Modules = []string{
	"attributegen",
}

// EnsureWasmFiles downloads wasm files for testing.
func EnsureWasmFiles(t *testing.T) {
	for _, module := range Modules {
		file := filepath.Join(testenv.IstioOut, fmt.Sprintf("%s.wasm", module))
		if _, err := os.Stat(file); err == nil {
			continue
		}
		url := fmt.Sprintf("https://storage.googleapis.com/istio-build/proxy/%s-%s.wasm", module, SHA)
		log.Printf("Downloading %s...\n", url)
		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatal(err)
		}
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(file, content, 0o666)
		if err != nil {
			t.Fatal(err)
		}
	}
}
