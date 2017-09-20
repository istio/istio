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
	"errors"
	"log"
	"reflect"
	"testing"
)

const (
	sutInit      = 1
	testSetup    = 2
	testRun      = 3
	testTeardown = 4
	sutCleanup   = 5
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

func newCommonConfig(testID string) (*CommonConfig, error) {
	t, err := newTestInfo(testID)
	if err != nil {
		return nil, err
	}
	c := &CommonConfig{
		Info:    t,
		Cleanup: new(testCleanup),
	}
	c.Cleanup.RegisterCleanable(c.Info)
	return c, nil
}

func newTestConfig() *testConfig {
	t := new(testConfig)
	t.s = new(sut)
	t.t = new(test)
	t.q = new([]int)
	t.s.queue = t.q
	t.t.queue = t.q
	return t
}

func (s *sut) Setup() error {
	if s.failSetup {
		return errors.New("init failed")
	}
	*s.queue = append(*s.queue, sutInit)
	return nil
}

func (s *sut) Teardown() error {
	if s.failTearDown {
		return errors.New("cleanup failed")
	}
	*s.queue = append(*s.queue, sutCleanup)
	return nil
}

func (c *test) Run() int {
	if c.failRun {
		return 1
	}
	*c.queue = append(*c.queue, testRun)
	return 0
}

func (c *test) Setup() error {
	if c.failSetup {
		return errors.New("setup failed")
	}
	*c.queue = append(*c.queue, testSetup)
	return nil
}

func (c *test) Teardown() error {
	if c.failTearDown {
		return errors.New("teardown failed")
	}
	*c.queue = append(*c.queue, testTeardown)
	return nil
}

func TestSuccess(t *testing.T) {
	c, err := newCommonConfig("test_success")
	if err != nil {
		t.Errorf("Error creating CommonConfig %s", err)
	}
	tc := newTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	if ret := c.RunTest(tc.t); ret != 0 {
		t.Errorf("non zero return value from RunTest")
	}
	b := []int{1, 2, 3, 4, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestFailure(t *testing.T) {
	c, err := newCommonConfig("test_failure")
	if err != nil {
		t.Errorf("Error creating CommonConfig %s", err)
	}
	tc := newTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failRun = true
	log.Printf("Expecting error, testing failure case")
	if ret := c.RunTest(tc.t); ret == 0 {
		t.Errorf("RunTest should have failed")
	}
	b := []int{1, 2, 4, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestInitFailure(t *testing.T) {
	c, err := newCommonConfig("test_init_failure")
	if err != nil {
		t.Errorf("Error creating CommonConfig %s", err)
	}
	tc := newTestConfig()
	tc.s.failSetup = true
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failRun = true
	log.Printf("Expecting error, testing init failure case")
	if ret := c.RunTest(tc.t); ret == 0 {
		t.Errorf("init should have failed during RunTest")
	}
	b := []int{5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestSetupFailure(t *testing.T) {
	c, err := newCommonConfig("test_setup_failure")
	if err != nil {
		t.Errorf("Error creating CommonConfig %s", err)
	}
	tc := newTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failSetup = true
	log.Printf("Expecting error, testing setup failure case")
	if ret := c.RunTest(tc.t); ret == 0 {
		t.Errorf("RunTest should have failed")
	}
	b := []int{1, 4, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestTearDownFailure(t *testing.T) {
	c, err := newCommonConfig("test_tear_down_failure")
	if err != nil {
		t.Errorf("Error creating CommonConfig %s", err)
	}
	tc := newTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.t.failTearDown = true
	log.Printf("Expecting error after RunTest, testing teardown failure case")
	if ret := c.RunTest(tc.t); ret != 0 {
		t.Errorf("RunTest should have passed since teardown happens after")
	}
	b := []int{1, 2, 3, 5}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}

func TestDeInitFailure(t *testing.T) {
	c, err := newCommonConfig("test_cleanup_failure")
	if err != nil {
		t.Errorf("Error creating CommonConfig %s", err)
	}
	tc := newTestConfig()
	c.Cleanup.RegisterCleanable(tc.s)
	c.Cleanup.RegisterCleanable(tc.t)
	tc.s.failTearDown = true
	if ret := c.RunTest(tc.t); ret != 0 {
		t.Errorf("RunTest should have passed")
	}
	b := []int{1, 2, 3, 4}
	if !reflect.DeepEqual(*tc.q, b) {
		t.Errorf("Order is not as expected %d %d", *tc.q, b)
	}
}
