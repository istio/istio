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
	"os"
	"testing"
	"time"
)

// dummyFileInfo is a dummy object to represent certain timestamp.
type dummyFileInfo struct {
	modTime     time.Time
	calledCount int
}

func (d *dummyFileInfo) Name() string {
	return "dummy"
}

func (d *dummyFileInfo) Size() int64 {
	return 0
}

func (d *dummyFileInfo) Mode() os.FileMode {
	return 0750
}

func (d *dummyFileInfo) ModTime() time.Time {
	return d.modTime
}

func (d *dummyFileInfo) IsDir() bool {
	return false
}

func (d *dummyFileInfo) Sys() interface{} {
	return nil
}

func TestFileClient(t *testing.T) {
	d := &dummyFileInfo{}
	var statErr error
	fc := &fileClient{
		opt: &Options{
			Path:           "test",
			UpdateInterval: time.Minute,
		},
		statFunc: func(path string) (os.FileInfo, error) {
			if statErr != nil {
				return nil, statErr
			}
			d.calledCount++
			return d, nil
		},
	}

	statErr = errors.New("dummy")
	if err := fc.GetStatus(); err != statErr {
		t.Errorf("Want %v, Got %v", statErr, err)
	}

	statErr = nil
	d.modTime = time.Now()
	if err := fc.GetStatus(); err != nil {
		t.Errorf("Want nil, Got %v", err)
	}
	if d.calledCount != 1 {
		t.Errorf("Want 1, Got %d", d.calledCount)
	}
	d.calledCount = 0

	d.modTime = time.Now().Add(-time.Hour)
	if err := fc.GetStatus(); err == nil {
		t.Error("Want error, Got nil")
	}
	if d.calledCount != 1 {
		t.Errorf("Want 1, Got %d", d.calledCount)
	}
}
