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

package framework

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

var (
	testLogsPath   = flag.String("test_logs_path", "", "Local path to store logs in")
	enableCoverage = flag.Bool("enable_coverage", false, "enable code coverage")
)

const (
	tmpPrefix   = "istio.e2e."
	idMaxLength = 36
)

// TestInfo gathers Test Information
type testInfo struct {
	RunID    string
	TestID   string
	TempDir  string
	Coverage coverage
}

type coverage struct {
	enabled bool
	path    string
}

func newCoverage(path string) coverage {
	var cov coverage
	if !*enableCoverage {
		return cov
	}
	cov.enabled = true
	cov.path = path
	return cov
}

// GetCoveragePath returns the path where to save coverage profile
func (c coverage) GetCoveragePath(module string) (string, error) {
	var p string
	if !c.enabled {
		return p, nil
	}
	p = path.Join(c.path, module)
	if err := os.MkdirAll(p, 0755); err != nil {
		return p, err
	}
	return p, nil
}

// NewTestInfo creates a TestInfo given a test id.
func newTestInfo(testID string) (*testInfo, error) {
	id, err := generateRunID(testID)
	if err != nil {
		return nil, err
	}
	var tmpDir string
	// testLogsPath will be used when called by Prow.
	// Bootstrap already gather stdout and stdin so we don't need to keep the logs from glog.
	if *testLogsPath != "" {
		tmpDir = path.Join(*testLogsPath, id)
		if err = os.MkdirAll(tmpDir, 0777); err != nil {
			return nil, err
		}
	} else {
		tmpDir, err = ioutil.TempDir(os.TempDir(), tmpPrefix)
		if err != nil {
			return nil, err
		}
	}
	log.Infof("Using temp dir %s", tmpDir)
	if err != nil {
		return nil, errors.New("could not create a temporary dir")
	}
	// Need to setup logging here
	return &testInfo{
		TestID:   testID,
		RunID:    id,
		TempDir:  tmpDir,
		Coverage: newCoverage(tmpDir),
	}, nil
}

func (t testInfo) Setup() error {
	return nil
}

func (t testInfo) FetchAndSaveClusterLogs(namespace string, kubeconfig string) error {
	return util.FetchAndSaveClusterLogs(namespace, t.TempDir, kubeconfig)
}

func (t testInfo) Teardown() error {
	return nil
}

func generateRunID(t string) (string, error) {
	u := uuid.New().String()
	u = strings.Replace(u, "-", "", -1)
	t = strings.Replace(t, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := idMaxLength - len(t)
	if padding < 6 {
		return "", fmt.Errorf("test name should be less that %d characters", idMaxLength-6)
	}
	return fmt.Sprintf("%s-%s", t, u[0:padding]), nil
}
