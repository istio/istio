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
	"sync"
	"time"

	"cloud.google.com/go/logging/apiv2"
	"cloud.google.com/go/storage"
	"github.com/golang/glog"
	"github.com/google/uuid"
	multierror "github.com/hashicorp/go-multierror"
	"google.golang.org/api/iterator"
	loggingpb "google.golang.org/genproto/googleapis/logging/v2"

	"istio.io/istio/tests/e2e/util"
)

var (
	logsBucketPath = flag.String("logs_bucket_path", "", "Cloud Storage Bucket path to use to store logs")
	projectID      = flag.String("project_id", "", "Project ID")
	resources      = []string{
		"pod",
		"service",
		"ingress",
	}
	testLogsPath = flag.String("test_logs_path", "", "Local path to store logs in")
	logIDs       = []string{
		"apiserver",
		"discovery",
		"istio-ingress",
		"mixer",
		"prometheus",
		"statesd-to-prometheus",
	}
)

const (
	tmpPrefix            = "istio.e2e."
	idMaxLength          = 36
	pageSize             = 1000 // number of log entries for each paginated request to fetch logs
	maxConcurrentWorkers = 1    //avoid overloading stackdriver api
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
	if *projectID == "" {
		return nil
	}
	// connect to stackdriver
	ctx := context.Background()
	glog.Info("Fetching cluster logs")
	loggingClient, err := logging.NewClient(ctx)
	if err != nil {
		return err
	}

	fetchAndWrite := func(logID string) error {
		// fetch logs from pods created for this run only
		filter := fmt.Sprintf(
			`logName = "projects/%s/logs/%s" AND
			resource.labels.namespace_id = "%s"`,
			*projectID, logID, namespace)
		req := &loggingpb.ListLogEntriesRequest{
			ResourceNames: []string{"projects/" + *projectID},
			Filter:        filter,
		}
		it := loggingClient.ListLogEntries(ctx, req)
		// create log file in append mode
		path := filepath.Join(t.LogsPath, fmt.Sprintf("%s.log", logID))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		defer func() {
			if err := f.Close(); err != nil {
				glog.Warningf("Error during closing file: %v\n", err)
			}
		}()
		// fetch log entries with pagination
		var entries []*loggingpb.LogEntry
		pager := iterator.NewPager(it, pageSize, "")
		for page := 0; ; page++ {
			pageToken, err := pager.NextPage(&entries)
			if err != nil {
				glog.Warningf("%s Iterator paging stops: %v", logID, err)
				return err
			}
			// append logs to file
			for _, logEntry := range entries {
				fmtTime := time.Unix(logEntry.GetTimestamp().Seconds, 0)
				timestamp := fmt.Sprintf("[%02d:%02d:%02d] ",
					fmtTime.Hour(), fmtTime.Minute(), fmtTime.Second())
				if _, err = f.WriteString(timestamp); err != nil {
					return err
				}
				log := logEntry.GetTextPayload()
				if _, err = f.WriteString(log); err != nil {
					return err
				}
				if len(log) == 0 || log[len(log)-1] != '\n' {
					if _, err = f.WriteString("\n"); err != nil {
						return err
					}
				}
			}
			if pageToken == "" {
				break
			}
		}
		return nil
	}

	var multiErr error
	var wg sync.WaitGroup
	// limit number of concurrent jobs to stay in stackdriver api quota
	jobQue := make(chan string, maxConcurrentWorkers)
	for _, logID := range logIDs {
		wg.Add(1)
		jobQue <- logID // blocked if jobQue channel is already filled
		// fetch logs in another go routine
		go func(logID string) {
			glog.Infof("Fetching logs on %s", logID)
			if err := fetchAndWrite(logID); err != nil {
				multiErr = multierror.Append(multiErr, err)
			}
			<-jobQue
			wg.Done()
		}(logID)
	}
	wg.Wait()

	for _, resrc := range resources {
		glog.Info(fmt.Sprintf("Fetching deployment info on %s\n", resrc))
		path := filepath.Join(t.LogsPath, fmt.Sprintf("%s.yaml", resrc))
		if yaml, err0 := util.Shell(fmt.Sprintf("kubectl get %s -n %s -o yaml", resrc, namespace)); err0 != nil {
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
