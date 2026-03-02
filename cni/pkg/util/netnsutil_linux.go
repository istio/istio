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

package util

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"

	netns "github.com/containernetworking/plugins/pkg/ns"
	"golang.org/x/sys/unix"
	"istio.io/istio/pkg/log"

	pconstants "istio.io/istio/cni/pkg/constants"
)

// RunAsHost executes the given function `f` within the host network namespace
func RunAsHost(f func() error) error {
	if f == nil {
		return nil
	}

	rl("EARLY", "/proc/self/ns/cgroup")
	// A network namespace switch is definitely not required in this case, which helps with testing
	if pconstants.HostNetNSPath == pconstants.SelfNetNSPath {
		return f()
	}
	rl("self pre", "/proc/self/ns/cgroup")
	return DoInCgroupNetNamespace(pconstants.HostCgroupNSPath, pconstants.HostNetNSPath, func() error {
		rl("self post", "/proc/self/ns/cgroup")
		//return netns.WithNetNSPath(pconstants.HostNetNSPath, func(_ netns.NetNS) error {
		//	rl("self in netns", "/proc/self/ns/cgroup")
		return f()
		//})
	})
}

func rl(name string, s string) {
	l, _ := os.Readlink(s)
	log.Errorf("howardjohn: LINK %v=%v", name, l)
	cmd := exec.Command("readlink", s)
	out, err := cmd.Output()
	log.Errorf("howardjohn: LINK exec %v=%v/%v", name, string(out), err)
}

func DoInCgroupNetNamespace(nsPath string, netnsPath string, toRun func() error) error {
	ns, err := netns.GetNS(nsPath)
	if err != nil {
		return err
	}
	defer ns.Close()
	nsnet, err := netns.GetNS(netnsPath)
	if err != nil {
		return err
	}
	defer nsnet.Close()
	log.Errorf("howardjohn: enter %v %v", nsPath, netnsPath)
	//if err := ns.errorIfClosed(); err != nil {
	//	return err
	//}

	containedCall := func() error {
		threadNS, err := getCurrentNSNoLock()
		if err != nil {
			return fmt.Errorf("failed to open current netns: %v", err)
		}
		threadNetNS, err := getCurrentNetNSNoLock()
		if err != nil {
			return fmt.Errorf("failed to open current netns: %v", err)
		}
		log.Errorf("howardjohn: current=%v new=%v", threadNS, ns)
		rl("cur pre", "/proc/self/ns/cgroup")
		rl("host pre", nsPath)
		defer threadNS.Close()
		defer threadNetNS.Close()

		// switch to target namespace
		if err = SetCgroup(ns); err != nil {
			return fmt.Errorf("error switching to ns %v: %v", nsPath, err)
		}
		if err = SetNet(nsnet); err != nil {
			return fmt.Errorf("error switching to ns %v: %v", nsPath, err)
		}
		defer func() {
			log.Errorf("howardjohn: set back...")
			err := SetCgroup(threadNS)  // switch back
			err2 := SetNet(threadNetNS) // switch back
			if err == nil && err2 == nil {
				// Unlock the current thread only when we successfully switched back
				// to the original namespace; otherwise leave the thread locked which
				// will force the runtime to scrap the current thread, that is maybe
				// not as optimal but at least always safe to do.
				runtime.UnlockOSThread()
			}
			rl("cur post switch", "/proc/self/ns/cgroup")
			log.Errorf("howardjohn: switch back... %v %v", err, err2)
		}()

		log.Errorf("howardjohn: RUN")
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

func getCurrentNSNoLock() (netns.NetNS, error) {
	return netns.GetNS(getCurrentThreadCgroupNSPath())
}

func getCurrentThreadCgroupNSPath() string {
	// /proc/self/ns/net returns the namespace of the main thread, not
	// of whatever thread this goroutine is running on.  Make sure we
	// use the thread's net namespace since the thread is switching around
	return fmt.Sprintf("/proc/%d/task/%d/ns/cgroup", os.Getpid(), unix.Gettid())
}

func getCurrentNetNSNoLock() (netns.NetNS, error) {
	return netns.GetNS(getCurrentThreadNetNSPath())
}

func getCurrentThreadNetNSPath() string {
	// /proc/self/ns/net returns the namespace of the main thread, not
	// of whatever thread this goroutine is running on.  Make sure we
	// use the thread's net namespace since the thread is switching around
	return fmt.Sprintf("/proc/%d/task/%d/ns/net", os.Getpid(), unix.Gettid())
}

func SetCgroup(ns netns.NetNS) error {
	//if err := ns.errorIfClosed(); err != nil {
	//	return err
	//}

	log.Errorf("howardjohn: ready to set!")
	//time.Sleep(time.Second*5)
	log.Errorf("howardjohn: setting! to fd %v", ns.Fd())
	rl("set to FD", fmt.Sprintf("/proc/self/fd/%d", ns.Fd()))

	if err := unix.Setns(int(ns.Fd()), unix.CLONE_NEWCGROUP); err != nil {
		return fmt.Errorf("Error switching to ns %v: %v", "WIP", err)
	}
	rl("self in SetCgroup", "/proc/self/ns/cgroup")

	return nil
}

func SetNet(ns netns.NetNS) error {
	//if err := ns.errorIfClosed(); err != nil {
	//	return err
	//}

	log.Errorf("howardjohn: ready to set!")
	//time.Sleep(time.Second*5)
	log.Errorf("howardjohn: setting! to fd %v", ns.Fd())
	rl("set to FD", fmt.Sprintf("/proc/self/fd/%d", ns.Fd()))

	if err := unix.Setns(int(ns.Fd()), unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("Error switching to ns %v: %v", "WIP", err)
	}
	rl("self in SetCgroup", "/proc/self/ns/cgroup")

	return nil
}
