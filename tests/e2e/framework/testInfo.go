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
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/golang/glog"
	"github.com/google/uuid"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/tests/util"
)

var (
	logsBucketPath = flag.String("logs_bucket_path", "", "Cloud Storage Bucket path to use to store logs")
	resources      = []string{
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
	RunID         string
	TestID        string
	Bucket        string
	LogBucketPath string
	LogsPath      string
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
	bucket := ""
	logsPath := ""
	var tmpDir string
	// testLogsPath will be used when called by Prow.
	// Bootstrap already gather stdout and stdin so we don't need to keep the logs from glog.
	if *testLogsPath != "" {
		tmpDir = path.Join(*testLogsPath, id)
		if err = os.MkdirAll(tmpDir, 0777); err != nil {
			return nil, err
		}
	} else {
		f := flag.Lookup("log_dir")
		tmpDir = f.Value.String()
		if tmpDir == "" {
			tmpDir, err = ioutil.TempDir(os.TempDir(), tmpPrefix)
			if err != nil {
				return nil, err
			}
		}
	}
	glog.Infof("Using log dir %s", tmpDir)

	if *logsBucketPath != "" {
		r := regexp.MustCompile(`gs://(?P<bucket>[^\/]+)/(?P<path>.+)`)
		m := r.FindStringSubmatch(*logsBucketPath)
		if m != nil {
			bucket = m[1]
			logsPath = m[2]
		} else {
			return nil, errors.New("cannot parse logBucketPath flag")
		}
	}
	if err != nil {
		return nil, errors.New("could not create a temporary dir")
	}
	// Need to setup logging here
	return &testInfo{
		TestID:        testID,
		RunID:         id,
		Bucket:        bucket,
		LogBucketPath: filepath.Join(logsPath, id),
		LogsPath:      tmpDir,
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
			path := filepath.Join(t.LogsPath, fmt.Sprintf("%s_container:%s.log", pod, container))
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			defer func() {
				if err = f.Close(); err != nil {
					glog.Warningf("Error during closing file: %v\n", err)
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

	lines, err := util.Shell("kubectl get pods -n " + namespace)
	if err != nil {
		return err
	}
	pods := strings.Split(lines, "\n")
	if len(pods) > 1 {
		for _, line := range pods[1:] {
			if idxEndOfPodName := strings.Index(line, " "); idxEndOfPodName > 0 {
				pod := line[:idxEndOfPodName]
				glog.Infof("Fetching logs on %s", pod)
				if err := fetchAndWrite(pod); err != nil {
					multiErr = multierror.Append(multiErr, err)
				}
			}
		}
	}

	for _, resrc := range resources {
		glog.Info(fmt.Sprintf("Fetching deployment info on %s\n", resrc))
		path := filepath.Join(t.LogsPath, fmt.Sprintf("%s.yaml", resrc))
		if yaml, err0 := util.ShellMuteOutput(
			fmt.Sprintf("kubectl get %s -n %s -o yaml", resrc, namespace)); err0 != nil {
			multiErr = multierror.Append(multiErr, err0)
		} else {
			if f, err1 := os.Create(path); err1 != nil {
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
	glog.Info("Creating status file")
	ts := testStatus{
		Status: r,
		Date:   time.Now(),
		TestID: t.TestID,
		RunID:  t.RunID,
	}
	fp := filepath.Join(t.LogsPath, fmt.Sprintf("%s.json", t.TestID))
	f, err := os.Create(fp)
	if err != nil {
		glog.Errorf("Could not create %s. Error %s", fp, err)
		return err
	}
	w := bufio.NewWriter(f)
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	if err = e.Encode(ts); err == nil {
		if err = w.Flush(); err == nil {
			if err = f.Close(); err == nil {
				glog.Infof("Created Status file %s", fp)
			}
		}
	}
	return err
}

func (t testInfo) uploadDir() error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		glog.Errorf("Could not set Storage client. Error %s", err)
		return err
	}
	bkt := client.Bucket(t.Bucket)

	uploadFileFn := func(p string) error {
		// Relative Path
		rp, err := filepath.Rel(t.LogsPath, p)
		if err != nil {
			return err
		}
		rp = filepath.Join(t.LogBucketPath, rp)
		glog.Infof("Uploading %s to gs://%s/%s", p, rp, t.Bucket)
		o := bkt.Object(rp)
		w := o.NewWriter(ctx)
		var b []byte
		if b, err = ioutil.ReadFile(p); err == nil {
			if _, err = w.Write(b); err == nil {
				if err = w.Close(); err == nil {
					glog.Infof("Uploaded %s to gs://%s/%s", p, rp, t.Bucket)
				}
			}
		}
		return err
	}

	walkFn := func(path string, info os.FileInfo, err error) error {
		// We are filtering all errors here as we want filepath.Walk to go over all the files.
		if err != nil {
			glog.Warningf("Skipping %s", path, err)
			return filepath.SkipDir
		}
		if !info.IsDir() {
			if uploadFileFn(path) != nil {
				glog.Warningf("An error occurred when upload %s %s", path, err)
			}
		}
		return nil
	}
	return filepath.Walk(t.LogsPath, walkFn)
}

func (t testInfo) Teardown() error {
	if t.Bucket != "" {
		glog.Info("Uploading logs remotely")
		glog.Flush()
		return t.uploadDir()
	}
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
