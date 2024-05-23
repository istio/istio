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
	vishnetns "github.com/vishvananda/netns"
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

func getInterfaceAddr(interfaceName string) (addr string, err error) {
	var (
		ief   *net.Interface
		addrs []net.Addr
	)
	if ief, err = net.InterfaceByName(interfaceName); err != nil {
		return
	}
	if addrs, err = ief.Addrs(); err != nil {
		return
	}
	for _, addr := range addrs {
		switch v := addr.(type) {
		case *net.IPNet:
			if v.IP.To4() != nil {
				return v.IP.String(), nil
			} else if v.IP.To16() != nil {
				return v.IP.String(), nil
			}
		}
	}
	return "", fmt.Errorf("interface %s doesn't have an IP address\n", interfaceName)
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
		ns := getPidNamespace(p.PID)
		id, err := vishnetns.GetFromPath(ns)
		defer id.Close()
		if err != nil {
			log.Warnf("failed to get netns for pid %v: %v", p.PID, err)
			continue
		}
		if err := vishnetns.Set(id); err != nil {
			log.Warnf("failed to switch to pid %v netns: %v", p.PID, err)
			continue
		}
		gip, err := getInterfaceAddr("eth0")
		if err != nil {
			log.Warnf("failed to read addr for eth0 in ns of pid %v ns: %v", p.PID, err)
			continue
		}

		if gip != pod.Status.PodIP {
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
