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
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"runtime"
	"sync"
	"testing"

	. "github.com/onsi/gomega"
)

func newWatchFile(t *testing.T) (string, func()) {
	g := NewGomegaWithT(t)

	watchDir, err := ioutil.TempDir("", "")
	g.Expect(err).NotTo(HaveOccurred())

	watchFile := path.Join(watchDir, "test.conf")
	err = ioutil.WriteFile(watchFile, []byte("foo: bar\n"), 0640)
	g.Expect(err).NotTo(HaveOccurred())

	cleanup := func() {
		os.RemoveAll(watchDir)
	}

	return watchFile, cleanup
}

// newTwoWatchFile returns with two watch files that exist in the same base dir.
func newTwoWatchFile(t *testing.T) (string, string, func()) {
	g := NewGomegaWithT(t)

	watchDir, err := ioutil.TempDir("", "")
	g.Expect(err).NotTo(HaveOccurred())

	watchFile1 := path.Join(watchDir, "test1.conf")
	err = ioutil.WriteFile(watchFile1, []byte("foo: bar\n"), 0640)
	g.Expect(err).NotTo(HaveOccurred())

	watchFile2 := path.Join(watchDir, "test2.conf")
	err = ioutil.WriteFile(watchFile2, []byte("foo: baz\n"), 0640)
	g.Expect(err).NotTo(HaveOccurred())

	cleanup := func() {
		os.RemoveAll(watchDir)
	}

	return watchFile1, watchFile2, cleanup
}

// newSymlinkedWatchFile simulates the behavior of k8s configmap/secret.
// Path structure looks like:
//      <watchDir>/test.conf
//                   ^
//                   |
// <watchDir>/data/test.conf
//             ^
//             |
// <watchDir>/data1/test.conf
func newSymlinkedWatchFile(t *testing.T) (string, string, func()) {
	g := NewGomegaWithT(t)

	watchDir, err := ioutil.TempDir("", "")
	g.Expect(err).NotTo(HaveOccurred())

	dataDir1 := path.Join(watchDir, "data1")
	err = os.Mkdir(dataDir1, 0777)
	g.Expect(err).NotTo(HaveOccurred())

	realTestFile := path.Join(dataDir1, "test.conf")
	t.Logf("Real test file location: %s\n", realTestFile)
	err = ioutil.WriteFile(realTestFile, []byte("foo: bar\n"), 0640)
	g.Expect(err).NotTo(HaveOccurred())

	cleanup := func() {
		os.RemoveAll(watchDir)
	}
	// Now, symlink the tmp `data1` dir to `data` in the baseDir
	os.Symlink(dataDir1, path.Join(watchDir, "data"))
	// And link the `<watchdir>/datadir/test.conf` to `<watchdir>/test.conf`
	watchFile := path.Join(watchDir, "test.conf")
	os.Symlink(path.Join(watchDir, "data", "test.conf"), watchFile)
	fmt.Printf("Watch file location: %s\n", path.Join(watchDir, "test.conf"))
	return watchDir, watchFile, cleanup
}

func TestWatchFile(t *testing.T) {
	t.Run("file content changed", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Given a file being watched
		watchFile, cleanup := newWatchFile(t)
		defer cleanup()
		_, err := os.Stat(watchFile)
		g.Expect(err).NotTo(HaveOccurred())

		w := NewWatcher()
		w.Add(watchFile)
		events := w.Events(watchFile)

		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			select {
			case <-events:
				wg.Done()
			}
		}()

		// Overwriting the file and waiting its event to be received.
		err = ioutil.WriteFile(watchFile, []byte("foo: baz\n"), 0640)
		wg.Wait()
	})

	t.Run("link to real file changed (for k8s configmap/secret path)", func(t *testing.T) {
		// skip if not executed on Linux
		if runtime.GOOS != "linux" {
			t.Skipf("Skipping test as symlink replacements don't work on non-linux environment...")
		}
		g := NewGomegaWithT(t)

		watchDir, watchFile, cleanup := newSymlinkedWatchFile(t)
		defer cleanup()

		w := NewWatcher()
		w.Add(watchFile)
		events := w.Events(watchFile)

		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			select {
			case <-events:
				wg.Done()
			}
		}()

		// Link to another `test.conf` file
		dataDir2 := path.Join(watchDir, "data2")
		err := os.Mkdir(dataDir2, 0777)
		g.Expect(err).NotTo(HaveOccurred())

		watchFile2 := path.Join(dataDir2, "test.conf")
		err = ioutil.WriteFile(watchFile2, []byte("foo: baz\n"), 0640)
		g.Expect(err).NotTo(HaveOccurred())

		// change the symlink using the `ln -sfn` command
		err = exec.Command("ln", "-sfn", dataDir2, path.Join(watchDir, "data")).Run()
		g.Expect(err).NotTo(HaveOccurred())

		// Wait its event to be received.
		wg.Wait()
	})
}

func TestWatcherLifecycle(t *testing.T) {
	g := NewGomegaWithT(t)

	watchFile1, watchFile2, cleanup := newTwoWatchFile(t)
	defer cleanup()

	w := NewWatcher()

	// Validate Add behavior
	err := w.Add(watchFile1)
	g.Expect(err).NotTo(HaveOccurred())
	err = w.Add(watchFile2)
	g.Expect(err).NotTo(HaveOccurred())
	err = w.Add(watchFile2)
	g.Expect(err).To(HaveOccurred())

	// Validate events and errors channel are fulfilled.
	events1 := w.Events(watchFile1)
	g.Expect(events1).NotTo(BeNil())
	events2 := w.Events(watchFile2)
	g.Expect(events2).NotTo(BeNil())

	errors1 := w.Errors(watchFile1)
	g.Expect(errors1).NotTo(BeNil())
	errors2 := w.Errors(watchFile2)
	g.Expect(errors2).NotTo(BeNil())

	// Validate Remove behavior
	err = w.Remove(watchFile1)
	g.Expect(err).NotTo(HaveOccurred())
	err = w.Remove(watchFile1)
	g.Expect(err).To(HaveOccurred())
	events1 = w.Events(watchFile1)
	g.Expect(events1).To(BeNil())
	errors1 = w.Errors(watchFile1)
	g.Expect(errors1).To(BeNil())
	events2 = w.Events(watchFile2)
	g.Expect(events2).NotTo(BeNil())
	errors2 = w.Errors(watchFile2)
	g.Expect(errors2).NotTo(BeNil())

	// Validate Close behavior
	err = w.Close()
	g.Expect(err).NotTo(HaveOccurred())
	events1 = w.Events(watchFile1)
	g.Expect(events1).To(BeNil())
	errors1 = w.Errors(watchFile1)
	g.Expect(errors1).To(BeNil())
	events2 = w.Events(watchFile2)
	g.Expect(events2).To(BeNil())
	errors2 = w.Errors(watchFile2)
	g.Expect(errors2).To(BeNil())
}
