// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nodeagent

import (
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/Microsoft/hcsshim/hcn"
)

type WindowsNamespace struct {
	ID          uint32
	GUID        string
	EndpointIds []string
}

// TODO: I don't think we actually need to implement the Linux interface
// the windows implementation is is probably separated enough that we don't
// need to.
type NamespaceCloser interface {
	NetnsCloser
	Namespaceable
}

type Namespaceable interface {
	Namespace() WindowsNamespace
}

type namespaceCloser struct {
	nsCloser io.Closer
	ns       WindowsNamespace
}

// TODO: maybe implement this for real if we need long-lived handles
// to namespaces, but I don't think we will
func (n *namespaceCloser) Close() error {
	if n.nsCloser == nil {
		return nil // no-op on Windows
	}
	return n.nsCloser.Close()
}

func (n *namespaceCloser) Fd() uintptr {
	panic("not implemented on windows OS")
}

func (n *namespaceCloser) Inode() uint64 {
	panic("not implemented on windows OS")
}

func (n *namespaceCloser) OwnerProcStarttime() uint64 {
	panic("not implemented on windows OS")
}

func (n *namespaceCloser) Namespace() WindowsNamespace {
	return n.ns
}

func NetnsDo(nsable Namespaceable, toRun func() error) error {
	containedCall := func() error {
		threadNS := hcn.GetCurrentThreadCompartmentId()
		if threadNS == 0 {
			return fmt.Errorf("failed to get current compartment id")
		}
		nsID := nsable.Namespace().ID
		err := hcn.SetCurrentThreadCompartmentId(nsID)
		if err != nil {
			return fmt.Errorf("failed to set compartment id: %v", err)
		}

		defer func() {
			// Move back to the host netns
			err := hcn.SetCurrentThreadCompartmentId(threadNS)
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
