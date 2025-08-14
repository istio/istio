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

package nodeagent

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"sync/atomic"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/iptables"
)

//go:embed testdata/cgroupns
var fakeProc embed.FS

type fakeZtunnel struct {
	deletedPods atomic.Int32
	addedPods   atomic.Int32
	addError    error
	delError    error
}

func (f *fakeZtunnel) Run(ctx context.Context) {
}

func (f *fakeZtunnel) PodDeleted(ctx context.Context, uid string) error {
	f.deletedPods.Add(1)
	return f.delError
}

func (f *fakeZtunnel) PodAdded(ctx context.Context, pod *corev1.Pod, netns Netns) error {
	f.addedPods.Add(1)
	return f.addError
}

func (f *fakeZtunnel) Close() error {
	return nil
}

// fakeNs is a mock struct for testing
type fakeNs struct {
	closed    *atomic.Bool
	fd        uintptr
	inode     uint64
	starttime uint64
}

func newFakeNs(fd uintptr) *fakeNs {
	// the fake inode is the fd! magic.
	return &fakeNs{closed: &atomic.Bool{}, fd: fd, inode: uint64(fd)}
}

func newFakeNsInode(fd uintptr, inode uint64) *fakeNs {
	return &fakeNs{closed: &atomic.Bool{}, fd: fd, inode: inode}
}

// Fd returns the file descriptor
func (f *fakeNs) Fd() uintptr {
	return f.fd
}

func (f *fakeNs) Inode() uint64 {
	return f.inode
}

func (f *fakeNs) OwnerProcStarttime() uint64 {
	return f.starttime
}

// Close simulates closing the file descriptor and returns nil for no error
func (f *fakeNs) Close() error {
	f.closed.Store(true)
	return nil
}

func fakeFs(uniqueInos bool) fs.FS {
	subFs, err := fs.Sub(fakeProc, "testdata")
	if err != nil {
		panic(err)
	}
	subFs, err = fs.Sub(subFs, "cgroupns")
	if err != nil {
		panic(err)
	}
	return &fakeFsWithFakeFds{ReadDirFS: subFs.(fs.ReadDirFS), uniqueInos: uniqueInos}
}

type fakeFsWithFakeFds struct {
	fs.ReadDirFS
	inoCounter int
	uniqueInos bool
}

// Open opens the named file.
// When Open returns an error, it should be of type *PathError
// with the Op field set to "open", the Path field set to name,
// and the Err field describing the problem.
//
// Open should reject attempts to open names that do not satisfy
// ValidPath(name), returning a *PathError with Err set to
// ErrInvalid or ErrNotExist.
func (ffs *fakeFsWithFakeFds) Open(name string) (fs.File, error) {
	f, err := ffs.ReadDirFS.Open(name)
	if err != nil {
		return nil, err
	}
	if ffs.uniqueInos {
		ffs.inoCounter++
	}
	return wrapFile(f, ffs.inoCounter), nil
}

func wrapFile(f fs.File, ino int) fs.File {
	return &fakeFileFakeFds{File: f, fd: 0, ino: ino}
}

type fakeFileFakeFds struct {
	fs.File
	fd  uintptr
	ino int
}

func (f *fakeFileFakeFds) Fd() uintptr {
	return f.fd
}

func (f *fakeFileFakeFds) Stat() (fs.FileInfo, error) {
	fi, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return &fakeFileFakeFI{FileInfo: fi, ino: f.ino}, nil
}

type fakeFileFakeFI struct {
	fs.FileInfo
	ino int
}

func (f *fakeFileFakeFI) Sys() any {
	return &syscall.Stat_t{Ino: uint64(f.ino), Dev: 3}
}

type fakeIptablesDeps struct {
	AddRouteErr           error
	AddInpodMarkIPRuleCnt atomic.Int32
	DelInpodMarkIPRuleCnt atomic.Int32
	AddLoopbackRoutesCnt  atomic.Int32
	DelLoopbackRoutesCnt  atomic.Int32
}

var _ iptables.NetlinkDependencies = &fakeIptablesDeps{}

func (r *fakeIptablesDeps) AddInpodMarkIPRule(cfg *config.IptablesConfig) error {
	r.AddInpodMarkIPRuleCnt.Add(1)
	return nil
}

func (r *fakeIptablesDeps) DelInpodMarkIPRule(cfg *config.IptablesConfig) error {
	r.DelInpodMarkIPRuleCnt.Add(1)
	return nil
}

func (r *fakeIptablesDeps) AddLoopbackRoutes(cfg *config.IptablesConfig) error {
	r.AddLoopbackRoutesCnt.Add(1)
	return r.AddRouteErr
}

func (r *fakeIptablesDeps) DelLoopbackRoutes(cfg *config.IptablesConfig) error {
	r.DelLoopbackRoutesCnt.Add(1)
	return nil
}

type NoOpPodNetnsProcFinder struct{}

func (p *NoOpPodNetnsProcFinder) FindNetnsForPods(pods map[types.UID]*corev1.Pod) (PodToNetns, error) {
	return make(PodToNetns), errors.New("NoOpPodNetnsProcFinder always returns an error")
}
