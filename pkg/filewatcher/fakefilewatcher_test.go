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

package filewatcher

import (
	"fmt"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestFakeFileWatcher(t *testing.T) {
	addedChan := make(chan string, 10)
	removedChan := make(chan string, 10)

	changed := func(path string, added bool) {
		if added {
			addedChan <- path
		} else {
			removedChan <- path
		}
	}

	newWatcher, fakeWatcher := NewFakeWatcher(changed)
	watcher := newWatcher()

	// verify Add()/Remove()
	for _, file := range []string{"foo", "bar", "baz"} {
		if err := watcher.Add(file); err != nil {
			t.Fatalf("Add() returned error: %v", err)
		}
		gotAdd := <-addedChan
		if gotAdd != file {
			t.Fatalf("Add() failed: got %v want %v", gotAdd, file)
		}
		select {
		case <-removedChan:
			t.Fatal("Add() failed: callback invoked with added=false")
		default:
		}

		wantEvent := fsnotify.Event{file, fsnotify.Write}
		fakeWatcher.InjectEvent(file, wantEvent)
		gotEvent := <-watcher.Events(file)
		if gotEvent != wantEvent {
			t.Fatalf("Events() failed: got %v want %v", gotEvent, wantEvent)
		}

		wantError := fmt.Errorf("error=%v", file)
		fakeWatcher.InjectError(file, wantError)
		gotError := <-watcher.Errors(file)
		if gotError != wantError {
			t.Fatalf("Errors() failed: got %v want %v", gotError, wantError)
		}

		if err := watcher.Remove(file); err != nil {
			t.Fatalf("Remove() returned error: %v", err)
		}
		gotRemove := <-removedChan
		if gotRemove != file {
			t.Fatalf("Remove() failed: got %v want %v", gotRemove, file)
		}
		select {
		case <-addedChan:
			t.Fatal("Remove() failed: callback invoked with added=false")
		default:
		}

		wantEvent = fsnotify.Event{file, fsnotify.Write}
		fakeWatcher.InjectEvent(file, wantEvent)
		select {
		case gotEvent := <-watcher.Events(file):
			t.Fatalf("Unexpected Events() after Remove(): got %v", gotEvent)
		default:
		}

		wantError = fmt.Errorf("error=%v", file)
		fakeWatcher.InjectError(file, wantError)
		select {
		case gotError := <-watcher.Errors(file):
			t.Fatalf("Unexpected Errors() after Remove(): got %v", gotError)
		default:
		}
	}

	// verify double Add() / Remove()
	for _, file := range []string{"foo2", "bar2", "baz2"} {
		if err := watcher.Add(file); err != nil {
			t.Fatalf("Add() returned error: %v", err)
		}
		if err := watcher.Add(file); err == nil {
			t.Fatal("Adding a path that already exists should fail")
		}
		if err := watcher.Remove(file); err != nil {
			t.Fatalf("Remove() returned error: %v", err)
		}
		if err := watcher.Remove(file); err == nil {
			t.Fatal("Removing a path that doesn't exist should fail")
		}
	}

	// verify Close()
	for _, file := range []string{"foo3", "bar3", "baz3"} {
		watcher.Add(file)
	}
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	for _, file := range []string{"foo2", "bar2", "baz2"} {
		wantEvent := fsnotify.Event{file, fsnotify.Write}
		fakeWatcher.InjectEvent(file, wantEvent)
		select {
		case gotEvent := <-watcher.Events(file):
			t.Fatalf("Unexpected Events() after Remove(): got %v", gotEvent)
		default:
		}

		wantError := fmt.Errorf("error=%v", file)
		fakeWatcher.InjectError(file, wantError)
		select {
		case gotError := <-watcher.Errors(file):
			t.Fatalf("Unexpected Errors() after Remove(): got %v", gotError)
		default:
		}
	}
}
