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
	TEST_TEARDOWN = 4
	SUT_CLEANUP   = 5
)

type test struct {
	queue        *[]int
	failSetup    bool
	failRun      bool
	failTearDown bool
}

type sut struct {
	queue        *[]int
	failSetup    bool
	failTearDown bool
}

type testConfig struct {
	q *[]int
	s *sut
	t *test
}

func NewTestConfig() *testConfig {
	t := new(testConfig)
	t.s = new(sut)
	t.t = new(test)
	t.q = new([]int)
	t.s.queue = t.q
	t.t.queue = t.q
	return t
}

func (s *sut) SetUp() error {
	if s.failSetup {
		return errors.New("Failed")
	}
	*s.queue = append(*s.queue, SUT_INIT)
	return nil
}

func (s *sut) TearDown() error {
	if s.failTearDown {
		return errors.New("Failed")
	}
	*s.queue = append(*s.queue, SUT_CLEANUP)
	return nil
}

func (c *test) Run() int {
	if c.failRun {
		return 1
	}
	*c.queue = append(*c.queue, TEST_RUN)
	return 0
}

func (c *test) SetUp() error {
	if c.failSetup {
		return errors.New("Failed")
	}
	*c.queue = append(*c.queue, TEST_SETUP)
	return nil
}

func (c *test) TearDown() error {
	if c.failTearDown {
		return errors.New("Failed")
	}
	*c.queue = append(*c.queue, TEST_TEARDOWN)
	return nil
}

func TestSuccess(t *testing.T) {
	c := NewCommonConfig("test-success")
	tc := NewTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	c.RunTest(tc.t)
	b := []int{1, 2, 3, 4, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestFailure(t *testing.T) {
	c := NewCommonConfig("test-failure")
	tc := NewTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failRun = true
	c.RunTest(tc.t)
	b := []int{1, 2, 4, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestInitFailure(t *testing.T) {
	c := NewCommonConfig("test-init-failure")
	tc := NewTestConfig()
	tc.s.failSetup = true
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failRun = true
	c.RunTest(tc.t)
	b := []int{5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestSetupFailure(t *testing.T) {
	c := NewCommonConfig("test-setup-failure")
	tc := NewTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failSetup = true
	c.RunTest(tc.t)
	b := []int{1, 4, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestTearDownFailure(t *testing.T) {
	c := NewCommonConfig("test-tear-down-failure")
	tc := NewTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failTearDown = true
	c.RunTest(tc.t)
	b := []int{1, 2, 3, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestDeInitFailure(t *testing.T) {
	c := NewCommonConfig("test-cleanup-failure")
	tc := NewTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.s.failTearDown = true
	c.RunTest(tc.t)
	b := []int{1, 2, 3, 4}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}
