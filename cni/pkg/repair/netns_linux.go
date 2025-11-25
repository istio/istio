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

package repair

import (
	"fmt"
	"math"
	"net"
	"strconv"

	netns "github.com/containernetworking/plugins/pkg/ns"
	"github.com/prometheus/procfs"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/log"
)

func getPidNamespace(pid int) string {
	return "/host/proc/" + strconv.Itoa(pid) + "/ns/net"
}

func runInHost[T any](f func() (T, error)) (T, error) {
	var res T
	ns := getPidNamespace(1)
	err := netns.WithNetNSPath(ns, func(_ netns.NetNS) error {
		var err error
		res, err = f()
		return err
	})
	if err != nil {
		return res, fmt.Errorf("in network namespace %v: %v", ns, err)
	}

	return res, nil
}

func checkInterfacesForMatchingAddr(targetAddr net.IP) (match bool, err error) {
	var addrs []net.Addr
	if addrs, err = net.InterfaceAddrs(); err != nil {
		return match, err
	}
	for _, addr := range addrs {
		switch v := addr.(type) {
		case *net.IPNet:
			if v.IP.Equal(targetAddr) {
				return true, nil
			}
		}
	}
	return false, fmt.Errorf("no interface has the address %s", targetAddr)
}

// getPodNetNs finds the network namespace for a given pod. There is not a great way to do this. Network namespaces live
// under the procfs, /proc/<pid>/ns/net. In majority of cases, this is not used directly, but is rather bind mounted to
// /var/run/netns/<name>. However, this pattern is not ubiquitous. Some platforms bind mount to other places. As we run
// in a pod, we cannot just access any arbitrary file they happen to bind mount in, as we don't know ahead of time where
// it might be.
//
// Instead, we rely directly on the procfs.
// This rules out two possible methods:
// * use crictl to inspect the pod; this returns the bind-mounted network namespace file.
// * /var/lib/cni/results shows the outputs of CNI plugins; this containers the bind-mounted network namespace file.
//
// Instead, we traverse the procfs. Comments on this method are inline.
func getPodNetNs(pod *corev1.Pod) (string, error) {
	parsedPodAddr := net.ParseIP(pod.Status.PodIP)
	if parsedPodAddr == nil {
		return "", fmt.Errorf("failed to parse addr: %s", pod.Status.PodIP)
	}

	fs, err := procfs.NewFS("/host/proc")
	if err != nil {
		return "", fmt.Errorf("read procfs: %v", err)
	}
	procs, err := fs.AllProcs()
	if err != nil {
		return "", fmt.Errorf("read procs: %v", err)
	}
	oldest := uint64(math.MaxUint64)
	best := ""

	// We will iterate over all processes. Our goal is to find a process whose namespace has a veth with an IP matching the pod.
	// There should be 1 or 2 processes that match: the pause container should always be there, and the istio-validation *might*.
	// We want the pause container, as the istio-validation one may exit before we are done.
	// We do this by detecting the longest running process. We could look at `cmdline`, but is likely more reliable to weird platforms.
	for _, p := range procs {
		match := false
		ns := getPidNamespace(p.PID)

		err := netns.WithNetNSPath(ns, func(_ netns.NetNS) error {
			var err error
			match, err = checkInterfacesForMatchingAddr(parsedPodAddr)
			return err
		})
		if err != nil {
			// It is expected this will fail for all but one of the procs
			continue
		}

		if !match {
			// Not the network we want, skip
			continue
		}
		s, err := p.Stat()
		if err != nil {
			// Unexpected... we will use it, but only if we find nothing without errors
			log.Warnf("failed to read proc %v stats: %v", p.PID, err)
			if best == "" {
				best = ns
			}
			continue
		}
		// Get the oldest one.
		if s.Starttime < oldest {
			oldest = s.Starttime
			best = ns
		}
	}
	if best == "" {
		return "", fmt.Errorf("failed to find network namespace")
	}
	return best, nil
}
