package framework

import (
	"flag"
	"github.com/golang/glog"
	"io/ioutil"
	"os"
	"sync"
)

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
	Setup() error
	Teardown() error
}

type Runnable interface {
	Run() int
}

// Hack to set the logging directory.
// Should be called right after flag.Parse().
func InitGlog() error {
	tmpDir, err := ioutil.TempDir(os.TempDir(), TMP_PREFIX)
	if err != nil {
		return err
	}
	f := flag.Lookup("log_dir")
	if err = f.Value.Set(tmpDir); err != nil {
		return err
	}
	glog.Info("Logging initialized")
	return nil
}

func NewCommonConfig(testId string) (*CommonConfig, error) {
	t, err := NewTestInfo(testId)
	if err != nil {
		return nil, err
	}
	c := &CommonConfig{
		Info:    t,
		Cleanup: new(TestCleanup),
	}
	c.Cleanup.RegisterCleanable(c.Info)
	return c, nil
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
		err := c.Setup()
		t.addCleanupAction(c.Teardown)
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
			glog.Errorf("Failed to cleanup. Error %s", err)
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
	glog.Info("Saving logs")
	if err := c.Info.CreateStatusFile(r); err == nil {

	} else {
		glog.Errorf("Could not create status file. Error %s", err)
		return err
	}
	return nil
}

func (c *CommonConfig) RunTest(m Runnable) int {
	ret := 1
	if err := c.Cleanup.Init(); err != nil {
		glog.Errorf("Failed to complete Init. Error %s", err)
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
