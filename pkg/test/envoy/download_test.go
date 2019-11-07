//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envoy_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/test/envoy"
)

func TestDownload(t *testing.T) {
	dir, err := ioutil.TempDir("", "envoy-download")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := envoy.DownloadLinuxRelease(dir); err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(filepath.Join(dir, "usr/local/bin/envoy"))
	if err != nil {
		t.Fatal(err)
	}
}
