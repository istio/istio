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

package binder

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testCredentialDir uses ioutil.TempDirectory to create a directory
func testCredentialDir() string {
	dir, err := ioutil.TempDir("", "test-watcher")
	if err != nil {
		panic(err)
	}
	return dir
}

func mkCredentialFile(path, uid string) string {
	credFile := filepath.Join(path, uid+CredentialsExtension)
	f, err := os.OpenFile(credFile, os.O_RDONLY|os.O_CREATE, 0666)
	if err != nil {
		panic(err)
	}
	f.Close()
	return credFile
}

func TestPollEmptyDir(t *testing.T) {
	dir := testCredentialDir()
	defer os.RemoveAll(dir)

	stopWatch := make(chan bool)
	rxChan := NewWatcher(dir).watch(stopWatch)
	defer func() {
		stopWatch <- true
	}()
	for i := 0; i < 3; i++ {
		select {
		case m := <-rxChan:
			t.Errorf("Did not expect to receive a message %v", m)
		default:
			time.Sleep(PollSleepTime)
		}
	}
}

func TestPollCreateFile(t *testing.T) {
	dir := testCredentialDir()
	defer os.RemoveAll(dir)

	//create credentials directory
	credDir := filepath.Join(dir, CredentialsSubdir)
	os.MkdirAll(credDir, 0777)

	mkCredentialFile(credDir, "1111-1111-1111")

	rxChan := NewWatcher(dir).watch(make(chan bool))
	got := false
	for i := 0; i < 3; i++ {
		select {
		case m := <-rxChan:
			if m.op != Added || m.uid != "1111-1111-1111" {
				t.Errorf("Expected Added event got %v", m)
			} else {
				got = true
			}
		default:
			time.Sleep(PollSleepTime)
		}
		if got == true {
			break
		}
	}
	if got == false {
		t.Errorf("Expected to get a Added event got none")
	}
}

func TestPollDeleteFile(t *testing.T) {
	dir := testCredentialDir()
	defer os.RemoveAll(dir)

	//create credentials directory
	credDir := filepath.Join(dir, CredentialsSubdir)
	os.MkdirAll(credDir, 0777)

	mkCredentialFile(credDir, "1111-1111-1111")
	rmUid := "1111-1111-1112"
	rmFile := mkCredentialFile(credDir, rmUid)

	rxChan := NewWatcher(dir).watch(make(chan bool))
	got := false
	for i := 0; i < 5; i++ {
		select {
		case m := <-rxChan:
			switch m.op {
			case Removed:
				if m.uid == rmUid {
					got = true
				}
			case Added:
				if m.uid == rmUid {
					os.Remove(rmFile)
					i = 0 // give the watcher a chance to catch up.
				}
			default:
			}
		default:
			time.Sleep(PollSleepTime)
		}
		if got == true {
			break
		}
	}

	if got == false {
		t.Errorf("Expected to get a delete event. Got none")
	}

}

// Check that we get added event for all the files
func TestPollSeededFiles(t *testing.T) {
	addedChecks := make(map[string]bool)
	addedChecks["1111-1111-1111"] = false
	addedChecks["1111-1111-1112"] = false
	addedChecks["1111-1111-1113"] = false

	dir := testCredentialDir()
	defer os.RemoveAll(dir)

	//create credentials directory
	credDir := filepath.Join(dir, CredentialsSubdir)
	os.MkdirAll(credDir, 0777)

	for uid, _ := range addedChecks {
		mkCredentialFile(credDir, uid)
	}

	rxChan := NewWatcher(dir).watch(make(chan bool))
	gotCount := len(addedChecks)
	for i := 0; i < 7; i++ {
		select {
		case m := <-rxChan:
			switch m.op {
			case Added:
				if _, ok := addedChecks[m.uid]; !ok {
					t.Errorf("Unexpected added %v", m)
				}
				if addedChecks[m.uid] == true {
					t.Errorf("Unexpected added again %v", m)
				}
				addedChecks[m.uid] = true
				gotCount -= 1
			case Removed:
				t.Errorf("Unexpected removed %v", m)
			default:
				t.Errorf("Unexpected op %v", m)
			}
		default:
			time.Sleep(PollSleepTime)
		}
		if gotCount == 0 {
			break
		}
	}

	if gotCount > 0 {
		t.Errorf("Expected to get events for all %d files (got %d)", len(addedChecks), len(addedChecks)-gotCount)
	}
}

// Test to check if we get both add and remove events
func TestPollAddAndRemoveFiles(t *testing.T) {
	type check struct {
		added        bool
		removed      bool
		expectedBoth bool
		fileName     string
	}
	checks := make(map[string]check)
	checks["1111-1111-1111"] = check{false, false, true, ""}
	checks["1111-1111-1112"] = check{false, false, true, ""}
	checks["1111-1111-1113"] = check{false, false, false, ""}
	// expecting two associated add's and removed.
	gotCount := 2

	dir := testCredentialDir()
	defer os.RemoveAll(dir)

	//create credentials directory
	credDir := filepath.Join(dir, CredentialsSubdir)
	os.MkdirAll(credDir, 0777)

	for uid, c := range checks {
		c.fileName = mkCredentialFile(credDir, uid)
		checks[uid] = c
	}

	rxChan := NewWatcher(dir).watch(make(chan bool))
	for i := 0; i < 8; i++ {
		select {
		case m := <-rxChan:
			switch m.op {
			case Added:
				if _, ok := checks[m.uid]; !ok {
					t.Errorf("Unexpected added %v", m)
				}
				c := checks[m.uid]
				c.added = true
				checks[m.uid] = c
				if c.expectedBoth == true {
					os.Remove(c.fileName)
					i = 0 // give watcher some more time.
				}
			case Removed:
				if _, ok := checks[m.uid]; !ok {
					t.Errorf("Unexpected delete %v", m)
				}
				c := checks[m.uid]
				if c.added == false {
					t.Errorf("Removed called before add for %s", m.uid)
				}
				c.removed = true
				checks[m.uid] = c
				if c.expectedBoth && (c.added && c.removed) {
					gotCount -= 1
				}
			default:
				t.Errorf("Unexpected op %v", m)
			}
		default:
			time.Sleep(PollSleepTime)
		}
		if gotCount == 0 {
			break
		}
	}

	for uid, c := range checks {
		if c.expectedBoth && (!c.added || !c.removed) {
			t.Errorf("Expected to get both added and removed event for %s (%v)/(%v)", uid, c.added, c.removed)
		}
	}
}
