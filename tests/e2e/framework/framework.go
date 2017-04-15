package framework

import (
	"flag"
	"github.com/golang/glog"
	"os"
	"testing"
)

var (
	logsBucketPath = flag.String("logs_bucket_path", "", "Cloud Storage Bucket path to use to store logs")
)

type TestConfig struct {
	RunId         string
	TestId        string
	LogBucketPath string
	LogsPath      string
}

type EndToEndTest interface {
	TestId() string
	SetTestConfig(*TestConfig)
	SetUp() error
	TearDown() error
}

func NewTestConfig(testId string) *TestConfig {
	return &TestConfig{
		TestId:        testId,
		RunId:         generateRunId(testId),
		LogBucketPath: *logsBucketPath,
		LogsPath:      os.TempDir(),
	}
}

func (c *TestConfig) setUp() error {
	// Create namespace
	// Deploy Istio
	glog.Info("TestConfig setup")
	return nil
}

func (c *TestConfig) tearDown() error {
	// Delete namespace
	glog.Info("TestConfig tear down")
	return nil
}

func (c *TestConfig) createStatusFile(r int) error {
	glog.Info("Creating status file")
	return nil
}

func (c *TestConfig) uploadLogs() error {
	glog.Info("Uploading log remotely")
	return nil
}

func (c *TestConfig) runTest(m *testing.M, t EndToEndTest) int {
	ret := 1
	defer c.uploadLogs()
	if err := c.setUp(); err == nil {
		if err = t.SetUp(); err == nil {
			ret = m.Run()
		} else {
			glog.Error("Failed to complete setup")
			ret = 1
		}
	} else {
		glog.Error("Failed to complete setup")
		ret = 1
	}
	if err := t.TearDown(); err != nil {
		glog.Error("Failed to complete teardown")
	}
	if err := c.tearDown(); err != nil {
		glog.Error("Failed to complete teardown")
	}
	if c.LogBucketPath != "" {
		c.createStatusFile(ret)
	}
	return ret
}

func generateRunId(t string) string {
	return "generatedId"

}

func setupLogging(logPath string) {
	f := flag.Lookup("log_dir")
	f.Value.Set(logPath)
}

func TestMain(m *testing.M, i EndToEndTest) {
	flag.Parse()
	t := NewTestConfig(i.TestId())
	setupLogging(t.LogsPath)
	i.SetTestConfig(t)
	os.Exit(t.runTest(m, i))
}
