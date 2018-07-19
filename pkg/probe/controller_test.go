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

const testDuration = 10 * time.Millisecond

var errClosed = errors.New("closed")

type dummyImpl struct {
	sync.Mutex
	lastStatus  error
	changeCount int
	updatec     chan struct{}
	closec      chan struct{}
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
	close(d.closec)
	return nil
}

func (d *dummyImpl) onUpdate(newStatus error) {
	d.Lock()
	defer d.Unlock()
	d.lastStatus = newStatus
	d.changeCount++
	select {
	case d.updatec <- struct{}{}:
	default:
	}
}

func (d *dummyImpl) wait() {
	<-d.updatec
	d.Lock()
	defer d.Unlock()
	close(d.updatec)
	d.updatec = make(chan struct{})
}

func newDummyController() (Controller, *dummyImpl) {
	d := &dummyImpl{updatec: make(chan struct{}), closec: make(chan struct{})}
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
	d.wait()
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	prevCount := d.GetCount()
	p1.SetAvailable(nil)
	d.wait()
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}
	if count := d.GetCount(); count-prevCount < 1 {
		t.Errorf("Count should be incremented (%d -> %d)", prevCount, count)
	}

	p2 := NewProbe()
	p2.RegisterProbe(c, "p2")
	d.wait()
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	p2.SetAvailable(nil)
	d.wait()
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}
	prevCount = d.GetCount()

	d.wait()
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %v, want nil", err)
	}
	if count := d.GetCount(); count-prevCount < 1 {
		t.Errorf("Count should be incremented (%d -> %d)", prevCount, count)
	}

	p1.SetAvailable(errors.New("dummy"))
	d.wait()
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	prevCount = d.GetCount()
	d.wait()
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
	d.wait()
	if err := d.GetStatus(); err == nil {
		t.Error("Got nil, want error")
	}

	p1.SetAvailable(nil)
	d.wait()
	if err := d.GetStatus(); err != nil {
		t.Errorf("Got %s, want nil", err)
	}

	if err := c.Close(); err != nil {
		t.Errorf("failed to close: %v", err)
	}
	<-d.closec
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

func TestNewFileController(t *testing.T) {
	d, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	path := filepath.Join(d, "fc")
	c := NewFileController(&Options{Path: path, UpdateInterval: testDuration})
	fc, ok := c.(*controller).impl.(*fileController)
	if !ok || fc == nil {
		t.Fatalf("NewFileController should return with fileController: %+v", fc)
	}
	if fc.path != path {
		t.Errorf("Want %s, Got %s", path, fc.path)
	}
}

func TestFileController(t *testing.T) {
	d, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	path := filepath.Join(d, "fc")
	fc := &fileController{path: path}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Got %v, Want not-existed", err)
	}
	fc.onUpdate(nil)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	fc.onUpdate(errors.New("dummy"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Got %v, Want not-existed", err)
	}
	fc.onUpdate(nil)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	fc.onUpdate(nil)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}

	if err := fc.onClose(); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Got %v, Want not-existed", err)
	}
	if err := fc.onClose(); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
}
