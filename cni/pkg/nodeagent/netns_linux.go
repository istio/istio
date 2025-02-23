//go:build linux
// +build linux

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
	"fmt"
	"runtime"
	"sync"

	netns "github.com/containernetworking/plugins/pkg/ns"
	"golang.org/x/sys/unix"
)

type NetnsWrapper struct {
	innerNetns         netns.NetNS
	inode              uint64
	ownerProcStarttime uint64
}

func (n *NetnsWrapper) Inode() uint64 {
	return n.inode
}

func (n *NetnsWrapper) Close() error {
	return n.innerNetns.Close()
}

func (n *NetnsWrapper) Fd() uintptr {
	return n.innerNetns.Fd()
}

func (n *NetnsWrapper) OwnerProcStarttime() uint64 {
	return n.ownerProcStarttime
}

func inodeForFd(n NetnsFd) (uint64, error) {
	stats := &unix.Stat_t{}
	err := unix.Fstat(int(n.Fd()), stats)
	return stats.Ino, err
}

func OpenNetns(nspath string) (NetnsCloser, error) {
	n, err := netns.GetNS(nspath)
	if err != nil {
		return nil, err
	}
	i, err := inodeForFd(n)
	if err != nil {
		n.Close()
		return nil, err
	}
	return &NetnsWrapper{innerNetns: n, inode: i}, nil
}

func NetnsSet(n NetnsFd) error {
	if err := unix.Setns(int(n.Fd()), unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("Error switching to ns fd %v: %v", n.Fd(), err)
	}
	return nil
}

// inspired by netns.Do() but with an existing fd.
func NetnsDo(fdable NetnsFd, toRun func() error) error {
	containedCall := func() error {
		threadNS, err := netns.GetCurrentNS()
		if err != nil {
			return fmt.Errorf("failed to open current netns: %v", err)
		}
		defer threadNS.Close()

		// switch to target namespace
		if err = NetnsSet(fdable); err != nil {
			return err
		}
		defer func() {
			err := threadNS.Set() // switch back
			if err == nil {
				// Unlock the current thread only when we successfully switched back
				// to the original namespace; otherwise leave the thread locked which
				// will force the runtime to scrap the current thread, that is maybe
				// not as optimal but at least always safe to do.
				runtime.UnlockOSThread()
			}
		}()

		return toRun()
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Start the callback in a new green thread so that if we later fail
	// to switch the namespace back to the original one, we can safely
	// leave the thread locked to die without a risk of the current thread
	// left lingering with incorrect namespace.
	var innerError error
	go func() {
		defer wg.Done()
		runtime.LockOSThread()
		innerError = containedCall()
	}()
	wg.Wait()

	return innerError
}
