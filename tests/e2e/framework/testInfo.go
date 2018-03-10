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
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/util"
)

var (
	resources = []string{
		"pod",
		"service",
		"ingress",
	}
	testLogsPath = flag.String("test_logs_path", "", "Local path to store logs in")
)

const (
	tmpPrefix   = "istio.e2e."
	idMaxLength = 36
)

// TestInfo gathers Test Information
type testInfo struct {
	RunID   string
	TestID  string
	TempDir string
}

// Test information
type testStatus struct {
	TestID string    `json:"test_id"`
	Status int       `json:"status"`
	RunID  string    `json:"run_id"`
	Date   time.Time `json:"date"`
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
		TestID:  testID,
		RunID:   id,
		TempDir: tmpDir,
	}, nil
}

func (t testInfo) Setup() error {
	return nil
}

// Update sets the test status.
func (t testInfo) Update(r int) error {
	return t.createStatusFile(r)
}

func (t testInfo) FetchAndSaveClusterLogs(namespace string) error {
	var multiErr error
	fetchAndWrite := func(pod string) error {
		cmd := fmt.Sprintf(
			"kubectl get pods -n %s %s -o jsonpath={.spec.containers[*].name}", namespace, pod)
		containersString, err := util.Shell(cmd)
		if err != nil {
			return err
		}
		containers := strings.Split(containersString, " ")
		for _, container := range containers {
			filePath := filepath.Join(t.TempDir, fmt.Sprintf("%s_container:%s.log", pod, container))
			f, err := os.Create(filePath)
			if err != nil {
				return err
			}
			defer func() {
				if err = f.Close(); err != nil {
					log.Warnf("Error during closing file: %v\n", err)
				}
			}()
			dump, err := util.ShellMuteOutput(
				fmt.Sprintf("kubectl logs %s -n %s -c %s", pod, namespace, container))
			if err != nil {
				return err
			}
			if _, err = f.WriteString(fmt.Sprintf("%s\n", dump)); err != nil {
				return err
			}
		}
		return nil
	}

	_, err := util.Shell("kubectl get ingress --all-namespaces")
	if err != nil {
		return err
	}
	lines, err := util.Shell("kubectl get pods -n " + namespace)
	if err != nil {
		return err
	}
	pods := strings.Split(lines, "\n")
	if len(pods) > 1 {
		for _, line := range pods[1:] {
			if idxEndOfPodName := strings.Index(line, " "); idxEndOfPodName > 0 {
				pod := line[:idxEndOfPodName]
				log.Infof("Fetching logs on %s", pod)
				if err := fetchAndWrite(pod); err != nil {
					multiErr = multierror.Append(multiErr, err)
				}
			}
		}
	}

	for _, resrc := range resources {
		log.Info(fmt.Sprintf("Fetching deployment info on %s\n", resrc))
		filePath := filepath.Join(t.TempDir, fmt.Sprintf("%s.yaml", resrc))
		if yaml, err0 := util.ShellMuteOutput(
			fmt.Sprintf("kubectl get %s -n %s -o yaml", resrc, namespace)); err0 != nil {
			multiErr = multierror.Append(multiErr, err0)
		} else {
			if f, err1 := os.Create(filePath); err1 != nil {
				multiErr = multierror.Append(multiErr, err1)
			} else {
				if _, err2 := f.WriteString(fmt.Sprintf("%s\n", yaml)); err2 != nil {
					multiErr = multierror.Append(multiErr, err2)
				}
			}
		}
	}
	return multiErr
}

func (t testInfo) createStatusFile(r int) error {
	log.Info("Creating status file")
	ts := testStatus{
		Status: r,
		Date:   time.Now(),
		TestID: t.TestID,
		RunID:  t.RunID,
	}
	fp := filepath.Join(t.TempDir, fmt.Sprintf("%s.json", t.TestID))
	f, err := os.Create(fp)
	if err != nil {
		log.Errorf("Could not create %s. Error %s", fp, err)
		return err
	}
	w := bufio.NewWriter(f)
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	if err = e.Encode(ts); err == nil {
		if err = w.Flush(); err == nil {
			if err = f.Close(); err == nil {
				log.Infof("Created Status file %s", fp)
			}
		}
	}
	return err
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
