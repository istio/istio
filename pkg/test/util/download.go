// Copyright 2019 Istio Authors.
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

package util

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"istio.io/pkg/log"
)

// HTTPDownload download from src(url) and store into dst(local file)
func HTTPDownload(dst string, src string) error {
	log.Infof("Start downloading from %s to %s ...\n", src, dst)
	var err error
	var out *os.File
	var resp *http.Response
	out, err = os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if err = out.Close(); err != nil {
			log.Errorf("Error: close file %s, %s", dst, err)
		}
	}()
	resp, err = http.Get(src)
	if err != nil {
		return err
	}
	defer func() {
		if err = resp.Body.Close(); err != nil {
			log.Errorf("Error: close downloaded file from %s, %s", src, err)
		}
	}()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http get request, received unexpected response status: %s", resp.Status)
	}
	if _, err = io.Copy(out, resp.Body); err != nil {
		return err
	}
	log.Info("Download successfully!")
	return err
}
