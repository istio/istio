// Copyright Istio Authors
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

package install

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"istio.io/istio/cni/pkg/config"
	testutils "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/test/util/assert"
)

// Antrea in chaining mode copies the primary into 05-antrea.conflist and re-copies it only
// on create/close_write of the primary's name. With a fixed primary, istio-cni must keep
// writing the primary, leave the lower copy alone, and raise close_write on the primary.
func TestCreateCNIConfigFileFixedPrimaryNotifiesCloseWrite(t *testing.T) {
	tempDir := t.TempDir()
	if err := file.AtomicCopy(filepath.Join("testdata", "list.conflist"), tempDir, "list.conflist"); err != nil {
		t.Fatal(err)
	}
	if err := file.AtomicCopy(filepath.Join("testdata", "list-with-istio.conflist"), tempDir, "05-antrea.conflist"); err != nil {
		t.Fatal(err)
	}
	lowerBefore := testutils.ReadFile(t, filepath.Join(tempDir, "05-antrea.conflist"))

	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.InotifyAddWatch(fd, tempDir, unix.IN_CLOSE_WRITE); err != nil {
		t.Fatal(err)
	}

	cfg := config.InstallConfig{
		CNIConfName:      "list.conflist",
		ChainedCNIPlugin: true,
		PluginLogLevel:   "debug",
		CNIAgentRunDir:   kubeconfigFilename,
		PodNamespace:     "my-namespace",
		MountedCNINetDir: tempDir,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resultFilepath, err := createCNIConfigFile(ctx, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, resultFilepath, filepath.Join(tempDir, "list.conflist"))

	goldenFilepath := filepath.Join("testdata", "list.conflist.golden")
	testutils.CompareBytes(t, testutils.ReadFile(t, resultFilepath), testutils.ReadFile(t, goldenFilepath), goldenFilepath)
	if !bytes.Equal(testutils.ReadFile(t, filepath.Join(tempDir, "05-antrea.conflist")), lowerBefore) {
		t.Fatal("lower-sorting config was modified")
	}

	if !closeWriteSeen(t, fd, "list.conflist") {
		t.Fatal("no close_write event on list.conflist")
	}
}

func closeWriteSeen(t *testing.T, fd int, name string) bool {
	t.Helper()
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	n, err := unix.Read(fd, buf)
	if err != nil {
		t.Fatal(err)
	}
	for off := 0; off+unix.SizeofInotifyEvent <= n; {
		ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
		nameBytes := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+int(ev.Len)]
		if ev.Mask&unix.IN_CLOSE_WRITE != 0 && string(bytes.TrimRight(nameBytes, "\x00")) == name {
			return true
		}
		off += unix.SizeofInotifyEvent + int(ev.Len)
	}
	return false
}
