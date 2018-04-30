//  Copyright 2018 Istio Authors
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

package tmp

import (
	"flag"
	"io/ioutil"
	"os"
	"path"

	"istio.io/istio/pkg/log"
)

const (
	// Prefix is the prefix used for generated test dir names.
	Prefix = "istio.e2e."
)

var (
	// TODO: This probably should simply be "log_target".
	testLogsPath = flag.String("test_logs_path", "", "Local path to store logs in")
)

// Create creates a temporary directory for the given test run ID.
func Create(runID string) (string, error) {
	var tmpDir string
	var err error

	// testLogsPath will be used when called by Prow.
	// Bootstrap already gather stdout and stdin so we don't need to keep the logs from glog.
	if *testLogsPath != "" {
		tmpDir = path.Join(*testLogsPath, runID)
		if err = os.MkdirAll(tmpDir, 0777); err != nil {
			return "", err
		}
	} else {
		tmpDir, err = ioutil.TempDir(os.TempDir(), Prefix)
		if err != nil {
			return "", err
		}
	}
	log.Infof("Using temp dir %s", tmpDir)

	return tmpDir, nil
}
