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
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/log"
)

func getPidNamespace(pid int) string {
	return "/host/proc/" + strconv.Itoa(pid) + "/ns/net"
}

func runInHost[T any](f func() (T, error)) (T, error) {
	var res T
	ns, err := netns.GetNS(getPidNamespace(1))
	if err != nil {
		return res, fmt.Errorf("failed to get host network: %v", err)
	}
	err = ns.Do(func(ns netns.NetNS) error {
		var err error
		res, err = f()
		return err
	})
	if err != nil {
		return res, fmt.Errorf("in host network: %v", err)
	}
	return res, nil
}

func findNetworkIDByIP(ip string) (int, error) {
	link, err := getLinkWithDestinationOf(ip)
	if err != nil {
		return 0, fmt.Errorf("find link for %v: %v", ip, err)
	}
	return link.Attrs().NetNsID, nil
}

func getLinkWithDestinationOf(ip string) (netlink.Link, error) {
	routes, err := netlink.RouteListFiltered(
		netlink.FAMILY_V4,
		&netlink.Route{Dst: &net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(32, 32)}},
		netlink.RT_FILTER_DST)
	if err != nil {
		return nil, err
	}

	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes found for %s", ip)
	}

	linkIndex := routes[0].LinkIndex
	return netlink.LinkByIndex(linkIndex)
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
	// First, find the network namespace id by looking the interface with the given Pod IP.
	// This could break on some platforms if they do not have an interface-per-pod.
	wantID, err := findNetworkIDByIP(pod.Status.PodIP)
	if err != nil {
		return "", fmt.Errorf("network id: %v", err)
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
	// We will iterate over all processes. Our goal is to find a process with the same network ID as we found above.
	// There should be 1 or 2 processes that match: the pause container should always be there, and the istio-validation *might*.
	// We want the pause container, as the istio-validation one may exit before we are done.
	// We do this by detecting the longest running process. We could look at `cmdline`, but is likely more reliable to weird platforms.
	for _, p := range procs {
		ns := getPidNamespace(p.PID)
		fd, err := unix.Open(ns, unix.O_RDONLY, 0)
		if err != nil {
			// Not uncommon, many processes are transient and we have a TOCTOU here.
			// No problem, must not be the one we are after.
			log.Debugf("failed to open pid %v: %v", p.PID, err)
			continue
		}
		id, err := netlink.GetNetNsIdByFd(fd)
		_ = unix.Close(fd)
		if err != nil {
			log.Debugf("failed to get netns for pid %v: %v", p.PID, err)
			continue
		}

		if id != wantID {
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
