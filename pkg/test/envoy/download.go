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

package envoy

import (
	"fmt"
	"net/http"

	"istio.io/istio/pkg/test/deps"
	"istio.io/istio/pkg/test/util"
)

var (
	LatestStableSHA string
	LinuxReleaseURL string
)

func init() {
	for _, dep := range deps.Istio {
		if dep.Name == "PROXY_REPO_SHA" {
			LatestStableSHA = dep.LastStableSHA
			LinuxReleaseURL = fmt.Sprintf("https://storage.googleapis.com/istio-build/proxy/envoy-alpha-%s.tar.gz", LatestStableSHA)
			return
		}
	}

	panic(fmt.Errorf("envoy SHA not found in: \n%v", deps.Istio))
}

// DownloadLinuxRelease downloads the release linux binary to the given directory.
func DownloadLinuxRelease(dir string) error {
	resp, err := http.Get(LinuxReleaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return util.ExtractTarGz(resp.Body, dir)
}
