package framework

import (
	"errors"
	"reflect"
	"testing"
)

const (
	SUT_INIT      = 1
	TEST_SETUP    = 2
	TEST_RUN      = 3
	SAVE_LOGS     = 4
	TEST_TEARDOWN = 5
	SUT_DEINIT    = 6
)

type test struct {
	queue        []int
	failInit     bool
	failSetup    bool
	failRun      bool
	failSaveLogs bool
	failTearDown bool
	failDeInit   bool
}

func (t *test) Init() error {
	if t.failInit {
		return errors.New("Failed")
	}
	t.queue = append(t.queue, SUT_INIT)
	return nil
}

func (t *test) DeInit() error {
	if t.failDeInit {
		return errors.New("Failed")
	}
	t.queue = append(t.queue, SUT_DEINIT)
	return nil
}

func (t *test) SaveLogs(r int) error {
	if t.failSaveLogs {
		return errors.New("Failed")
	}
	t.queue = append(t.queue, SAVE_LOGS)
	return nil
}

func (c *test) Run() int {
	if c.failRun {
		return 1
	}
	c.queue = append(c.queue, TEST_RUN)
	return 0
}

func (c *test) SetUp() error {
	if c.failSetup {
		return errors.New("Failed")
	}
	c.queue = append(c.queue, TEST_SETUP)
	return nil
}

func (c *test) TearDown() error {
	if c.failTearDown {
		return errors.New("Failed")
	}
	c.queue = append(c.queue, TEST_TEARDOWN)
	return nil
}

func TestSuccess(t *testing.T) {
	a := new(test)
	RunTest(a, a, a)
	b := []int{1, 2, 3, 4, 5, 6}
	if !reflect.DeepEqual(a.queue, b) {
		t.Errorf("Order is not as expected %d %d", a.queue, b)
	}
}

func TestFailure(t *testing.T) {
	a := new(test)
	a.failRun = true
	RunTest(a, a, a)
	b := []int{1, 2, 4, 5, 6}
	if !reflect.DeepEqual(a.queue, b) {
		t.Errorf("Order is not as expected %d %d", a.queue, b)
	}
}

func TestInitFailure(t *testing.T) {
	a := new(test)
	a.failInit = true
	RunTest(a, a, a)
	b := []int{4, 6}
	if !reflect.DeepEqual(a.queue, b) {
		t.Errorf("Order is not as expected %d %d", a.queue, b)
	}
}

func TestSetupFailure(t *testing.T) {
	a := new(test)
	a.failSetup = true
	RunTest(a, a, a)
	b := []int{1, 4, 5, 6}
	if !reflect.DeepEqual(a.queue, b) {
		t.Errorf("Order is not as expected %d %d", a.queue, b)
	}
}

func TestSaveLogsFailure(t *testing.T) {
	a := new(test)
	a.failSaveLogs = true
	RunTest(a, a, a)
	b := []int{1, 2, 3, 5, 6}
	if !reflect.DeepEqual(a.queue, b) {
		t.Errorf("Order is not as expected %d %d", a.queue, b)
	}
}

func TestTearDownFailure(t *testing.T) {
	a := new(test)
	a.failTearDown = true
	RunTest(a, a, a)
	b := []int{1, 2, 3, 4, 6}
	if !reflect.DeepEqual(a.queue, b) {
		t.Errorf("Order is not as expected %d %d", a.queue, b)
	}
}

func TestDeInitFailure(t *testing.T) {
	a := new(test)
	a.failDeInit = true
	RunTest(a, a, a)
	b := []int{1, 2, 3, 4, 5}
	if !reflect.DeepEqual(a.queue, b) {
		t.Errorf("Order is not as expected %d %d", a.queue, b)
	}
}
