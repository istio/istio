// Copyright 2016 Google Inc.
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

package kube

import (
	"errors"
	"testing"
	"time"
)

func TestQueue(t *testing.T) {
	q := NewQueue(1 * time.Microsecond)
	stop := make(chan struct{})
	out := 0
	err := true
	add := func(obj interface{}) error {
		t.Logf("adding %d, error: %t", obj.(int), err)
		out = out + obj.(int)
		if !err {
			return nil
		}
		err = false
		return errors.New("intentional error")
	}
	go q.Run(stop)
	q.Push(Task{handler: add, obj: 1})
	q.Push(Task{handler: add, obj: 2})
	time.Sleep(100 * time.Microsecond)
	close(stop)
	if out != 4 {
		t.Errorf("Queue => %d, want %d", out, 4)
	}
}
