package framework

import (
	"bufio"
	"cloud.google.com/go/storage"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/golang/glog"
	"github.com/google/uuid"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	logsBucketPath = flag.String("logs_bucket_path", "", "Cloud Storage Bucket path to use to store logs")
)

const (
	TMP_PREFIX    = "istio.e2e."
	ID_MAX_LENGTH = 36
)

type TestInfo struct {
	RunId         string
	TestId        string
	Bucket        string
	LogBucketPath string
	LogsPath      string
}

type TestStatus struct {
	TestId string    `json:"test_id"`
	Status int       `json:"status"`
	RunId  string    `json:"run_id"`
	Date   time.Time `json:"date"`
}

func NewTestInfo(testId string) (*TestInfo, error) {
	bucket := ""
	logsPath := ""
	f := flag.Lookup("log_dir")
	tmpDir := f.Value.String()
	if tmpDir == "" {
		var err error

		tmpDir, err = ioutil.TempDir(os.TempDir(), TMP_PREFIX)
		if err != nil {
			return nil, err
		}

	}
	glog.Infof("Using log dir %s", tmpDir)
	id, err := generateRunId(testId)
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
			return nil, errors.New("Cannot parse logBucketPath flag")
		}
	}
	if err != nil {
		return nil, errors.New("Could not create a Temporary dir")
	}
	// Need to setup logging here
	return &TestInfo{
		TestId:        testId,
		RunId:         id,
		Bucket:        bucket,
		LogBucketPath: filepath.Join(logsPath, id),
		LogsPath:      tmpDir,
	}, nil
}

func (t TestInfo) Setup() error {
	return nil
}

func (t TestInfo) CreateStatusFile(r int) error {
	glog.Info("Creating status file")
	ts := TestStatus{
		Status: r,
		Date:   time.Now(),
		TestId: t.TestId,
		RunId:  t.RunId,
	}
	fp := filepath.Join(t.LogsPath, fmt.Sprintf("%s.json", t.TestId))
	f, err := os.Create(fp)
	if err != nil {
		glog.Errorf("Could not create %s. Error %s", fp, err)
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	err = e.Encode(ts)
	w.Flush()
	glog.Infof("Created Status file %s", fp)
	return err
}

func (t TestInfo) uploadDir() error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		glog.Errorf("Could not set Storage client. Error %s", err)
		return err
	}
	bkt := client.Bucket(t.Bucket)

	uploadFile := func(p string) error {
		// Relative Path
		rp, err := filepath.Rel(t.LogsPath, p)
		if err != nil {
			return err
		}
		rp = filepath.Join(t.LogBucketPath, rp)
		glog.Infof("Uploading %s to gs://%s/%s", p, rp, t.Bucket)
		o := bkt.Object(rp)
		w := o.NewWriter(ctx)
		defer w.Close()
		b, err := ioutil.ReadFile(p)
		_, err = w.Write(b)
		return err
	}

	walkFn := func(path string, info os.FileInfo, err error) error {
		// We are filtering all errors here as we want filepath.Walk to go over all the files.
		if err != nil {
			glog.Warningf("Skipping %s", path, err)
			return filepath.SkipDir
		}
		if !info.IsDir() {
			if uploadFile(path) != nil {
				glog.Warningf("An error occured when upload %s %s", path, err)
			}
		}
		return nil
	}
	return filepath.Walk(t.LogsPath, walkFn)
}

func (t TestInfo) Teardown() error {
	if t.Bucket != "" {
		glog.Info("Uploading logs remotely")
		glog.Flush()
		return t.uploadDir()
	}
	return nil
}

func generateRunId(t string) (string, error) {
	u := uuid.New().String()
	u = strings.Replace(u, "-", "", -1)
	t = strings.Replace(t, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := ID_MAX_LENGTH - len(t)
	if padding < 6 {
		return "", errors.New(
			fmt.Sprintf("Test Name should be less that %d characters", ID_MAX_LENGTH-6))
	}
	return fmt.Sprintf("%s-%s", t, u[0:padding]), nil
}
