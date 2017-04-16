package framework

import (
	"flag"
	"github.com/golang/glog"
	"io/ioutil"
	"os"
	"testing"
)

var (
	logsBucketPath = flag.String("logs_bucket_path", "", "Cloud Storage Bucket path to use to store logs")
)

type TestInfo struct {
	RunId         string
	TestId        string
	LogBucketPath string
	LogsPath      string
}

type Test interface {
	TestId() string
	SetTestInfo(*TestInfo)
	SetUp() error
	TearDown() error
}

type Sut interface {
	SetUp() error
	TearDown() error
	SaveLogs(int) error
}

func NewTestInfo(testId string) *TestInfo {
	tmpDir, err := ioutil.TempDir(os.TempDir(), "istio.e2e.")
	if err != nil {
		glog.Fatal("Could not create a Temporary dir")
	}
	return &TestInfo{
		TestId:        testId,
		RunId:         generateRunId(testId),
		LogBucketPath: *logsBucketPath,
		LogsPath:      tmpDir,
	}
}

func (t *TestInfo) SetUp() error {
	// Create namespace
	// Deploy Istio
	glog.Info("SUT setup")
	return nil
}

func (t *TestInfo) TearDown() error {
	// Delete namespace
	glog.Info("SUT teardown")
	return nil
}

func (t *TestInfo) SaveLogs(r int) error {
	if t.LogBucketPath == "" {
		return nil
	}
	// Delete namespace
	glog.Info("SUT savelogs")
	if err := t.createStatusFile(r); err == nil {
		if err = t.uploadLogs(); err != nil {
			glog.Error("Could not save logs")
			return err
		}

	} else {
		glog.Error("Could not create status file")
		return err
	}
	return nil
}

func (t *TestInfo) createStatusFile(r int) error {
	glog.Info("Creating status file")
	return nil
}

func (t *TestInfo) uploadLogs() error {
	glog.Info("Uploading log remotely")
	glog.Flush()
	return nil
}

func RunTest(m *testing.M, s Sut, t Test) int {
	ret := 1
	if err := s.SetUp(); err == nil {
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
	s.SaveLogs(ret)
	if err := t.TearDown(); err != nil {
		glog.Error("Failed to complete teardown")
	}
	if err := s.TearDown(); err != nil {
		glog.Error("Failed to complete teardown")
	}
	return ret
}

func generateRunId(t string) string {
	return "generatedId"

}

func setupLogging(logPath string) {
	// Hack to set the logging directory. No logging should be done before calling this.
	f := flag.Lookup("log_dir")
	f.Value.Set(logPath)
	glog.Info("Using log path ", logPath)
}

func TestMain(m *testing.M, t Test) {
	flag.Parse()
	s := NewTestInfo(t.TestId())
	setupLogging(s.LogsPath)
	t.SetTestInfo(s)
	os.Exit(RunTest(m, s, t))
}
