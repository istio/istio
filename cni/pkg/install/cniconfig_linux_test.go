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
	"sync/atomic"
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

	fd := watchCloseWrite(t, tempDir)

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

	if !closeWriteSeen(t, fd) {
		t.Fatal("no close_write event on list.conflist")
	}
}

// Uninstall in chained mode strips istio-cni from the primary; a chaining CNI that copied
// the primary must see that, or its copy keeps naming the removed istio-cni binary.
func TestCleanupChainedNotifiesCloseWrite(t *testing.T) {
	cniNetDir := t.TempDir()
	cniConfigFilePath := filepath.Join(cniNetDir, "list.conflist")
	if err := file.AtomicCopy(filepath.Join("testdata", "list-with-istio.conflist"), cniNetDir, "list.conflist"); err != nil {
		t.Fatal(err)
	}
	fd := watchCloseWrite(t, cniNetDir)

	isReady := &atomic.Value{}
	isReady.Store(false)
	installer := NewInstaller(&config.InstallConfig{
		MountedCNINetDir: cniNetDir,
		ChainedCNIPlugin: true,
		CNIBinTargetDirs: []string{t.TempDir()},
	}, isReady)
	installer.cniConfigFilepath = cniConfigFilePath
	if err := installer.Cleanup(); err != nil {
		t.Fatal(err)
	}

	goldenFilepath := filepath.Join("testdata", "list-no-istio.conflist")
	testutils.CompareBytes(t, testutils.ReadFile(t, cniConfigFilePath), testutils.ReadFile(t, goldenFilepath), goldenFilepath)
	if !closeWriteSeen(t, fd) {
		t.Fatal("no close_write event on list.conflist")
	}
}

func TestRemoveIstioCNIFromPrimaryNotifiesCloseWrite(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "list.conflist")
	if err := file.AtomicCopy(filepath.Join("testdata", "list-with-istio.conflist"), dir, "list.conflist"); err != nil {
		t.Fatal(err)
	}
	fd := watchCloseWrite(t, dir)

	if err := removeIstioCNIFromPrimary(primary, 0o644); err != nil {
		t.Fatal(err)
	}

	goldenFilepath := filepath.Join("testdata", "list-no-istio.conflist")
	testutils.CompareBytes(t, testutils.ReadFile(t, primary), testutils.ReadFile(t, goldenFilepath), goldenFilepath)
	if !closeWriteSeen(t, fd) {
		t.Fatal("no close_write event on list.conflist")
	}
}

// A config that is already current must not be rewritten or notified: a CNI that copies our
// config on every close_write would otherwise loop with us.
func TestUnchangedConfigIsNotNotified(t *testing.T) {
	tempDir := t.TempDir()
	if err := file.AtomicCopy(filepath.Join("testdata", "list.conflist"), tempDir, "list.conflist"); err != nil {
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
	if _, err := createCNIConfigFile(ctx, &cfg); err != nil {
		t.Fatal(err)
	}

	fd := watchCloseWrite(t, tempDir)
	if _, err := createCNIConfigFile(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	if closeWriteSeen(t, fd) {
		t.Fatal("unchanged list.conflist raised close_write")
	}
}

func watchCloseWrite(t *testing.T, dir string) int {
	t.Helper()
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	if _, err := unix.InotifyAddWatch(fd, dir, unix.IN_CLOSE_WRITE); err != nil {
		t.Fatal(err)
	}
	return fd
}

// closeWriteSeen reports whether list.conflist raised close_write.
func closeWriteSeen(t *testing.T, fd int) bool {
	t.Helper()
	buf := make([]byte, 64*(unix.SizeofInotifyEvent+unix.NAME_MAX+1))
	n, err := unix.Read(fd, buf)
	if err == unix.EAGAIN {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	for off := 0; off+unix.SizeofInotifyEvent <= n; {
		ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
		nameBytes := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+int(ev.Len)]
		if ev.Mask&unix.IN_CLOSE_WRITE != 0 && string(bytes.TrimRight(nameBytes, "\x00")) == "list.conflist" {
			return true
		}
		off += unix.SizeofInotifyEvent + int(ev.Len)
	}
	return false
}
