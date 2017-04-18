package framework

import (
	"flag"
	"github.com/golang/glog"
	"io/ioutil"
	"os"
	"sync"
)

var (
	logsBucketPath = flag.String("logs_bucket_path", "", "Cloud Storage Bucket path to use to store logs")
)

const (
	TMP_PREFIX = "istio.e2e."
)

type TestInfo struct {
	RunId         string
	TestId        string
	LogBucketPath string
	LogsPath      string
}

type TestCleanup struct {
	Cleanables         []Cleanable
	CleanablesLock     sync.Mutex
	CleanupActions     []func() error
	CleanupActionsLock sync.Mutex
}

type CommonConfig struct {
	Cleanup *TestCleanup
	Info    *TestInfo
	// Other common config here.
}

type Cleanable interface {
	SetUp() error
	TearDown() error
}

type Runnable interface {
	Run() int
}

func NewCommonConfig(testId string) *CommonConfig {
	tmpDir, err := ioutil.TempDir(os.TempDir(), TMP_PREFIX)
	if err != nil {
		glog.Fatal("Could not create a Temporary dir")
	}

	c := &CommonConfig{
		Info: &TestInfo{
			TestId:        testId,
			RunId:         generateRunId(testId),
			LogBucketPath: *logsBucketPath,
			LogsPath:      tmpDir,
		},
		Cleanup: new(TestCleanup),
	}
	c.Cleanup.RegisterCleanable(c.Info)
	return c
}

func (t *TestCleanup) RegisterCleanable(c Cleanable) {
	t.CleanablesLock.Lock()
	defer t.CleanablesLock.Unlock()
	t.Cleanables = append(t.Cleanables, c)
}

func (t *TestCleanup) getCleanable() Cleanable {
	t.CleanablesLock.Lock()
	defer t.CleanablesLock.Unlock()
	if len(t.Cleanables) == 0 {
		return nil
	}
	c := t.Cleanables[0]
	t.Cleanables = t.Cleanables[1:]
	return c
}

func (t *TestCleanup) addCleanupAction(fn func() error) {
	t.CleanupActionsLock.Lock()
	defer t.CleanupActionsLock.Unlock()
	t.CleanupActions = append(t.CleanupActions, fn)
}

func (t *TestCleanup) getCleanupAction() func() error {
	t.CleanupActionsLock.Lock()
	defer t.CleanupActionsLock.Unlock()
	if len(t.CleanupActions) == 0 {
		return nil
	}
	fn := t.CleanupActions[len(t.CleanupActions)-1]
	t.CleanupActions = t.CleanupActions[:len(t.CleanupActions)-1]
	return fn
}

func (t *TestCleanup) Init() error {
	// Run setup on all cleanable
	glog.Info("Starting Initialization")
	c := t.getCleanable()
	for c != nil {
		err := c.SetUp()
		t.addCleanupAction(c.TearDown)
		if err != nil {
			return err
		}
		c = t.getCleanable()
	}
	glog.Info("Initialization complete")
	return nil
}

func (t *TestCleanup) Cleanup() {
	// Run tear down on all cleanable
	glog.Info("Starting Cleanup")
	fn := t.getCleanupAction()
	for fn != nil {
		if err := fn(); err != nil {
			glog.Errorf("Failed to cleanup ", err)
		}
		fn = t.getCleanupAction()
	}
	glog.Info("Cleanup complete")
}

func (c *CommonConfig) SaveLogs(r int) error {
	if c.Info == nil {
		glog.Warning("Skipping log saving as Info is not initialized")
		return nil
	}
	if c.Info.LogBucketPath == "" {
		return nil
	}
	// Delete namespace
	glog.Info("Saving logs")
	if err := c.Info.createStatusFile(r); err == nil {

	} else {
		glog.Error("Could not create status file")
		return err
	}
	return nil
}

func (c *CommonConfig) RunTest(m Runnable) int {
	ret := 1
	if err := c.Cleanup.Init(); err != nil {
		glog.Error("Failed to complete Init")
		ret = 1
	} else {
		glog.Info("Running test")
		ret = m.Run()
	}
	if err := c.SaveLogs(ret); err != nil {
		glog.Warning("Failed to save logs")
	}
	c.Cleanup.Cleanup()
	return ret
}

func (t TestInfo) SetUp() error {
	glog.Info("Creating status file")
	setupLogging(t.LogsPath)
	return nil
}

func (t TestInfo) createStatusFile(r int) error {
	glog.Info("Creating status file")
	return nil
}

func (t TestInfo) TearDown() error {
	glog.Info("Uploading log remotely")
	glog.Flush()
	return nil
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
