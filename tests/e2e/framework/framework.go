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
	Init() error
	DeInit() error
	SaveLogs(int) error
}

type Runnable interface {
	Run() int
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

func (t *TestInfo) Init() error {
	// Create namespace
	// Deploy Istio
	glog.Info("SUT setup")
	return nil
}

func (t *TestInfo) DeInit() error {
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

func RunTest(m Runnable, s Sut, t Test) int {
	ret := 1
	runTearDown := true
	if err := s.Init(); err == nil {
		if err = t.SetUp(); err == nil {
			ret = m.Run()
		} else {
			glog.Error("Failed to complete Setup")
			ret = 1
		}
	} else {
		glog.Error("Failed to complete Init")
		runTearDown = false
		ret = 1
	}
	if err := s.SaveLogs(ret); err != nil {
		glog.Warning("Failed to save logs")
	}
	if runTearDown {
		if err := t.TearDown(); err != nil {
			glog.Error("Failed to complete Teardown")
		}
	}
	if err := s.DeInit(); err != nil {
		glog.Error("Failed to complete DeInit")
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

func E2eTestMain(m *testing.M, t Test) {
	flag.Parse()
	s := NewTestInfo(t.TestId())
	setupLogging(s.LogsPath)
	t.SetTestInfo(s)
	os.Exit(RunTest(m, s, t))
}
