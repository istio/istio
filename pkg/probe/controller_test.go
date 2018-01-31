// Copyright 2018 Istio Authors
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

package probe

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const (
	testDuration     = 10 * time.Millisecond
	testFileDuration = 100 * time.Millisecond
)

var errClosed = errors.New("closed")

type dummyImpl struct {
	sync.Mutex
	lastStatus  error
	changeCount int
	updatec     chan struct{}
}

func (d *dummyImpl) GetStatus() error {
	d.Lock()
	defer d.Unlock()
	return d.lastStatus
}

func (d *dummyImpl) GetCount() int {
	d.Lock()
	defer d.Unlock()
	return d.changeCount
}

func (d *dummyImpl) onClose() error {
	d.Lock()
	defer d.Unlock()
	d.lastStatus = errClosed
	return nil
}

func (d *dummyImpl) onUpdate(newStatus error) {
	d.Lock()
	defer d.Unlock()
	d.lastStatus = newStatus
	d.changeCount++
	if d.updatec != nil {
		select {
		case d.updatec <- struct{}{}:
		default:
		}
	}
}

func (d *dummyImpl) wait(t *testing.T, timeout time.Duration) {
	t.Helper()
	d.Lock()
	if d.updatec != nil {
		close(d.updatec)
	}
	d.updatec = make(chan struct{})
	d.Unlock()
	for i := 0; i < 10; i++ {
		select {
		case <-d.updatec:
			return
		case <-time.After(timeout):
		}
	}
	t.Fatal("Failed to wait")
}

func newDummyController() (Controller, *dummyImpl) {
	d := &dummyImpl{}
	c := &controller{
		statuses: map[*Probe]error{},
		name:     "dummy",
		interval: testDuration,
		impl:     d,
	}
	c.Start()
	return c, d
}

func TestController(t *testing.T) {
	c, d := newDummyController()
	defer c.Close()

	p1 := NewProbe()
	p1.RegisterProbe(c, "p1")
	time.Sleep(testDuration)
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	prevCount := d.GetCount()
	p1.SetAvailable(nil)
	d.wait(t, testDuration)
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}
	if count := d.GetCount(); count-prevCount < 1 {
		t.Errorf("Count should be incremented (%d -> %d)", prevCount, count)
	}

	p2 := NewProbe()
	p2.RegisterProbe(c, "p2")
	d.wait(t, testDuration)
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	p2.SetAvailable(nil)
	d.wait(t, testDuration)
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}
	prevCount = d.GetCount()

	d.wait(t, testDuration*2)
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}
	if count := d.GetCount(); count-prevCount < 1 {
		t.Errorf("Count should be incremented (%d -> %d)", prevCount, count)
	}

	p1.SetAvailable(errors.New("dummy"))
	d.wait(t, testDuration)
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	prevCount = d.GetCount()
	d.wait(t, testDuration*2)
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}
	if count := d.GetCount(); count-prevCount < 1 {
		t.Errorf("Count should be incremented (%d -> %d)", prevCount, count)
	}
}

func TestControllerRegisterTwice(t *testing.T) {
	c, _ := newDummyController()
	defer c.Close()

	getSize := func() int {
		cc := c.(*controller)
		cc.Lock()
		defer cc.Unlock()
		return len(cc.statuses)
	}
	p1 := NewProbe()
	p1.RegisterProbe(c, "p1")
	if size := getSize(); size != 1 {
		t.Errorf("Got %v, Want 1", size)
	}
	p1.RegisterProbe(c, "p1")
	if size := getSize(); size != 1 {
		t.Errorf("Got %v, Want 1", size)
	}
	p1.RegisterProbe(c, "p2")
	if size := getSize(); size != 1 {
		t.Errorf("Got %v, Want 1", size)
	}
}

func TestControllerAfterClose(t *testing.T) {
	c, d := newDummyController()

	p1 := NewProbe()
	p1.RegisterProbe(c, "p1")
	d.wait(t, testDuration)
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	p1.SetAvailable(nil)
	d.wait(t, testDuration)
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %s, want nil", err)
	}

	if err := c.Close(); err != nil {
		t.Errorf("failed to close: %v", err)
	}
	time.Sleep(testDuration * 2)
	if err := d.GetStatus(); err != errClosed {
		t.Errorf("Got %v, Want %v", err, errClosed)
	}

	p1.SetAvailable(errors.New("dummy"))
	time.Sleep(testDuration * 2)
	if err := d.GetStatus(); err != errClosed {
		t.Errorf("Got %v, Want %v", err, errClosed)
	}
	p1.SetAvailable(nil)
	time.Sleep(testDuration * 2)
	if err := d.GetStatus(); err != errClosed {
		t.Errorf("Got %v, Want %v", err, errClosed)
	}
}

func TestFileControllerMethods(t *testing.T) {
	d, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	path := filepath.Join(d, "fc")
	fc := &fileController{path: path}
	client := NewFileClient(&Options{path, testFileDuration})
	if err := client.GetStatus(); err == nil {
		t.Error("Got nil, Want error")
	}
	fc.onUpdate(nil)
	if err := client.GetStatus(); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	time.Sleep(testFileDuration * 3)
	if err := client.GetStatus(); err == nil {
		t.Error("Got nil, Want error")
	}
	fc.onUpdate(nil)
	if err := client.GetStatus(); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	fc.onUpdate(errors.New("dummy"))
	if err := client.GetStatus(); !os.IsNotExist(err) {
		t.Errorf("Got %v, Want not-existed", err)
	}
}

func TestFileController(t *testing.T) {
	d, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	opt := &Options{filepath.Join(d, "fc"), testFileDuration}
	fc := NewFileController(opt)
	client := NewFileClient(opt)
	fc.Start()
	defer fc.Close()

	p1 := NewProbe()
	p1.RegisterProbe(fc, "p1")
	time.Sleep(testFileDuration)
	if err := client.GetStatus(); err == nil {
		t.Errorf("Got nil, want error")
	}

	p1.SetAvailable(nil)
	time.Sleep(testFileDuration)
	if err := client.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}

	p2 := NewProbe()
	p2.RegisterProbe(fc, "p2")
	time.Sleep(testFileDuration)
	if err := client.GetStatus(); err == nil {
		t.Errorf("Got nil, want error")
	}

	p2.SetAvailable(nil)
	time.Sleep(testFileDuration)
	if err := client.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}

	time.Sleep(testFileDuration * 2)
	if err := client.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}

	p1.SetAvailable(errors.New("dummy"))
	time.Sleep(testFileDuration)
	if err := client.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	time.Sleep(testFileDuration * 2)
	if err := client.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}
}
