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
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"cloud.google.com/go/storage"
	"github.com/golang/glog"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"

	"istio.io/istio/tests/e2e/util"
)

var (
	logsBucketPath = flag.String("logs_bucket_path", "", "Cloud Storage Bucket path to use to store logs")
	projectID = flag.String("project_id", "istio-testing", "Project ID")
	// This list of log identifiers is cherry-picked based on Dev team's interest
	// To access the comprehensive list, try in your shell
	// 		$ gcloud beta logging logs list
	logIDs = []string{
		"apiserver",
		"discovery",
		"istio-ingress",
		"mixer",
		"prometheus",
		"statesd-to-prometheus",
	}
	resources = []string{
		"pod",
		"service",
		"ingress",
	}
)

const (
	tmpPrefix   = "istio.e2e."
	idMaxLength = 36
	pageSize = 20
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
	bucket := ""
	logsPath := ""
	f := flag.Lookup("log_dir")
	tmpDir := f.Value.String()
	if tmpDir == "" {
		var err error

		tmpDir, err = ioutil.TempDir(os.TempDir(), tmpPrefix)
		if err != nil {
			return nil, err
		}

	}
	glog.Infof("Using log dir %s", tmpDir)
	id, err := generateRunID(testID)
	if err != nil {
		return nil, err
	}
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

func (t testInfo) FetchAndSaveClusterLogs() error {
	ctx := context.Background()
	glog.Info("Fetching cluster logs")
	client, err := logadmin.NewClient(ctx, *projectID)
	if err != nil {
	    return err
	}

	fetchAndWrite := func(logId string) error {
		glog.Info(fmt.Sprintf("Fetching logs on %s\n", logId))
		// fetch logs from pods created for this run only
		filter := fmt.Sprintf(
			`logName = "projects/%s/logs/%s" AND
			resource.labels.namespace_id = "%s"`,
			*projectID, logId, *namespace)
		it := client.Entries(ctx, logadmin.Filter(filter))
		// create log file in append mode
		path := filepath.Join(t.LogsPath, fmt.Sprintf("%s.log", logId))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		// fetch log entries with pagination 
		var entries []*logging.Entry
		pager := iterator.NewPager(it, pageSize, "")
		for page := 0; ; page++ {
			pageToken, err := pager.NextPage(&entries)
			if err != nil {
				glog.Errorf("Iterator paging failed: %v", err)
				return err
			}
			// append logs to file
			for _, logEntry := range entries {
				_, err = f.WriteString(fmt.Sprintf("%v\n", logEntry.Payload))
				if err != nil {
					return err
				}
			}
			if pageToken == "" {
				break
			}
		}
		return f.Close()
	}

	errMsg := ""
	for _, logID := range logIDs {
		if err := fetchAndWrite(logID); err != nil {
			errMsg += err.Error() + "\n"
		}
	}
	for _, resrc := range resources {
		glog.Info(fmt.Sprintf("Fetching deployment info on %s\n", resrc))
		path := filepath.Join(t.LogsPath, fmt.Sprintf("%s.yaml", resrc))
		cmd := fmt.Sprintf("kubectl get %s -o yaml >> %s", resrc, path)
		if _, err := util.Shell(cmd); err != nil {
			errMsg += err.Error()+ "\n"
		}
	}
	if errMsg != "" {
		return fmt.Errorf("%s\n", errMsg)
	}
	return nil
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
