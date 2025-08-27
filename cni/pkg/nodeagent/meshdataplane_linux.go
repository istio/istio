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
	"context"
	"errors"
	"net/netip"

	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	set "istio.io/istio/cni/pkg/addressset"
	"istio.io/istio/cni/pkg/trafficmanager"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/util/sets"
)

type meshDataplane struct {
	kubeClient         kubernetes.Interface
	netServer          MeshDataplane
	hostTrafficManager trafficmanager.TrafficRuleManager
	hostAddrSet        set.AddressSetManager
}

// ConstructInitialSnapshot is always called first, before Start.
// It takes a "snapshot" of ambient pods that were already running when the server started,
// and constructs various required "state" (adding the pods to the host-level node ipset,
// building the state of the world snapshot send to connecting ztunnels)
func (s *meshDataplane) ConstructInitialSnapshot(existingAmbientPods []*corev1.Pod) error {
	if err := s.syncHostAddrSets(existingAmbientPods); err != nil {
		log.Errorf("failed to sync host addressSet: %v", err)
		return err
	}

	return s.netServer.ConstructInitialSnapshot(existingAmbientPods)
}

// ConstructInitialSnapshot should always be invoked before this function.
func (s *meshDataplane) Start(ctx context.Context) {
	s.netServer.Start(ctx)
}

// Stop terminates the netserver, flushes host ipsets, and removes host iptables healthprobe rules.
func (s *meshDataplane) Stop(skipCleanup bool) {
	// Remove host rules (or not) that allow pod healthchecks to work.
	// These are not critical but if they are not in place pods that have
	// already been captured will eventually start to fail kubelet healthchecks.
	if !skipCleanup {
		log.Info("CNI ambient server terminating, cleaning up node net rules")

		log.Debug("removing host traffic rules")
		s.hostTrafficManager.DeleteHostRules()
		_ = util.RunAsHost(func() error {
			log.Debug("destroying host addressSet")
			s.hostAddrSet.Flush()
			if err := s.hostAddrSet.DestroySet(); err != nil {
				log.Warnf("could not destroy host addressSet on shutdown")
			}
			return nil
		})
	}

	s.netServer.Stop(skipCleanup)
}

// AddPodToMesh attempts to inject iptables rules into the pod, and sends the pod to ztunnel.
// If this succeeds, it will add the pod to the node ipset, and finally annotate the pod
// (indicating it has been mutated/captured) with a captured status.
//
// If anything fails *before* sending the pod to ztunnel, returns an NonRetryableAdd error
// (which indicates the function call should not be retried).
//
// For any other failure after that point, returns a standard Error (which indicates the
// function call can be retried).
//
// If the *last* step of sending the pod to ztunnel fails, then the pod will still be annotated
// with a partially-captured status (indicating it has been mutated/redirected, and thus potentially
// needs cleanup) and the error will be returned, indicating that the function call can be retried.
func (s *meshDataplane) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	// Ordering is important in this func:
	//
	// - Inject rules and add to ztunnel FIRST
	// - Annotate IF rule injection doesn't fail.
	// - Add pod IP to ipset IF none of the above has failed, as a last step
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	if err := s.netServer.AddPodToMesh(ctx, pod, podIPs, netNs); err != nil {
		// iptables injection failed, this is not a "retryable partial add"
		// this is a nonrecoverable/nonretryable error and we won't even bother to
		// annotate the pod or retry the event.
		if errors.Is(err, ErrNonRetryableAdd) {
			// propagate, bail immediately, don't continue
			return err
		}
		// Any other error is recoverable/retryable and means we just couldn't contact ztunnel.
		// However, any other error also means iptables injection was successful, so
		// we must annotate the pod with a partial status (so removal can undo iptables)
		// regardless of ztunnel's current status.
		// So annotate indicating that this pod was injected (and thus needs to be either retried
		// or uninjected on removal) but is not fully captured yet.
		log.Error("failed to add pod to ztunnel: pod partially added, annotating with pending status")
		if err := util.AnnotatePartiallyEnrolledPod(s.kubeClient, &pod.ObjectMeta); err != nil {
			// If we have an error annotating the partial status - that is itself retryable.
			return err
		}
		// Otherwise return the original error
		return err
	}

	// Handle node healthcheck probe rewrites
	if _, err := s.addPodToHostAddrSet(pod, podIPs); err != nil {
		log.Errorf("failed to add pod to addressSet, pod will fail healthchecks: %v", err)
		// Adding pod to ipset should always be an upsert, so should not fail
		// unless we have a kernel incompatibility - thus it should either
		// never fail, or isn't usefully retryable.
		// For now tho, err on the side of being loud in the logs,
		// since retrying in that case isn't _harmful_ and means all pods will fail anyway.
		return err
	}

	// Once we successfully annotate the pod as fully captured,
	// the informer can no longer retry events, and there's no going back.
	// This should be the last step in all cases - once we do this, the CP (and the informer) will consider
	// this pod "ambient" and successfully enrolled.
	log.Debugf("annotating pod")
	if err := util.AnnotateEnrolledPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		// If we have an error annotating the full status - that is retryable.
		// (maybe K8S is busy, etc - but we need a k8s controlplane ACK).
		return err
	}

	return nil
}

// RemovePodFromMesh attempts to remove iptables rules from the pod (if it is not already terminating),
// and sends the pod remove to ztunnel.
//
// If this succeeds, it will un-annotate the pod (indicating it is no longer captured) and remove the pod
// from the node ipset.
//
// If anything fails, an error will be returned, indicating that the function call can be retried.
func (s *meshDataplane) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	log.WithLabels("deleted", isDelete).Info("removing pod from mesh")

	// Remove the hostside ipset entry first, and unconditionally - if later failures happen, we never
	// want to leave stale entries (and the pod healthchecks will start to fail, which is a good signal)
	if err := removePodFromHostAddrSet(pod, s.hostAddrSet); err != nil {
		log.Errorf("failed to remove pod %s from host addressSet, error was: %v", pod.Name, err)
		// Removing pod from ipset should never fail, even if the IP is no longer there
		// (unless we have a kernel incompatibility).
		// - so while retrying on ipser remove error is safe from an eventing perspective,
		// it may not be useful
		return err
	}

	// Specifically, at this time, we do _not_ want to un-annotate the pod if we cannot successfully remove it,
	// as the pod is probably in a broken state and we don't want to let go of it from the CP perspective while that is true.
	// So we will return if this fails (for a potential retry).
	if err := s.netServer.RemovePodFromMesh(ctx, pod, isDelete); err != nil {
		log.Errorf("failed to remove pod from mesh: %v", err)
		return err
	}

	// This should be the last step in all cases - once we do this, the CP will no longer consider
	// this pod "ambient" (and the informer will not be able to retry on removal errors),
	// regardless of the state it is in.
	log.Debug("removing annotation from pod")
	if err := util.AnnotateUnenrollPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		log.Errorf("failed to annotate pod unenrollment: %v", err)
		// If the pod is already terminating anyway, we don't care if we can't remove the annotation,
		// so only return a retryable error on annotation failure if it's not terminating.
		if !isDelete {
			// If we have an error annotating the partial status and the pod is not terminating
			// - that is retryable.
			return err
		}
	}

	return nil
}

// syncHostAddrSets is called after the host node ipset has been created (or found + flushed)
// during initial snapshot creation, it will insert every snapshotted pod's IP into the set.
//
// The set does not allow dupes (obviously, that would be undefined) - but in the real world due to misconfigured
// IPAM or other things, we may see two pods with the same IP on the same node - we will skip the dupes,
// which is all we can do - these pods will fail healthcheck until the IPAM issue is resolved (which seems reasonable)
func (s *meshDataplane) syncHostAddrSets(ambientPods []*corev1.Pod) error {
	var addedIPSnapshot []netip.Addr

	for _, pod := range ambientPods {
		podIPs := util.GetPodIPsIfPresent(pod)
		if len(podIPs) == 0 {
			log.Warnf("pod %s does not appear to have any assigned IPs, not syncing with ipset", pod.Name)
		} else {
			addedIps, err := s.addPodToHostAddrSet(pod, podIPs)
			if err != nil {
				log.Errorf("pod %s has IP collision (%v), pod will be skipped and will fail healthchecks: %v", pod.Name, podIPs, err)
			}
			addedIPSnapshot = append(addedIPSnapshot, addedIps...)
		}

	}
	return pruneHostAddrSet(sets.New(addedIPSnapshot...), s.hostAddrSet)
}

// addPodToHostAddrSet:
// 1. get pod manifest
// 2. Get all pod ips (might be several, v6/v4)
// 3. update ipsets accordingly
// 4. return the ones we added successfully, and errors for any we couldn't (dupes)
//
// For each set (v4, v6), each pod IP can appear exactly once.
//
// Note that for adds, because we have 2 potential sources of async adds (CNI plugin and informer)
// we want this to be an upsert.
//
// This is important to make sure reconciliation and overlapping events do not cause problems -
// adds can come from two sources, but removes can only come from one source (informer).
//
// Ex:
// Pod UID "A", IP 1.1.1.1
// Pod UID "B", IP 1.1.1.1
//
// Add for UID "B", IP 1.1.1.1 is handled before Remove for UID "A", IP 1.1.1.1
// -> we no longer have an entry for either, which is bad (pod fails healthchecks)
//
// So "add" always overwrites, and remove only removes if the pod IP AND the pod UID match.
func (s *meshDataplane) addPodToHostAddrSet(pod *corev1.Pod, podIPs []netip.Addr) ([]netip.Addr, error) {
	// Add the pod UID as an ipset entry comment, so we can (more) easily find and delete
	// all relevant entries for a pod later.
	podUID := string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name, "podUID", podUID, "ipset", s.hostAddrSet.GetPrefix())

	var ipsetAddrErrs []error
	var addedIps []netip.Addr

	err := util.RunAsHost(func() error {
		// For each pod IP
		for _, pip := range podIPs {
			// Add to host ipset
			log.Debugf("adding probe ip %s to addressSet", pip)
			if err := s.hostAddrSet.AddIP(pip, ipProto, podUID, true); err != nil {
				ipsetAddrErrs = append(ipsetAddrErrs, err)
				log.Errorf("failed adding ip %s to addressSet, error was %s", pip, err)
			} else {
				addedIps = append(addedIps, pip)
			}
		}
		return nil
	})
	if err != nil {
		ipsetAddrErrs = append(ipsetAddrErrs, err)
	}

	return addedIps, errors.Join(ipsetAddrErrs...)
}

// removePodFromHostAddrSet will remove (v4, v6) pod IPs from the host IP set(s).
// Note that unlike when we add the IP to the set, on removal we will simply
// skip removing the IP if the IP matches, but the UID comment does not match our pod.
func removePodFromHostAddrSet(pod *corev1.Pod, hostsideProbeSet set.AddressSetManager) error {
	podUID := string(pod.ObjectMeta.UID)
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name, "podUID", podUID, "ipset", hostsideProbeSet.GetPrefix())

	podIPs := util.GetPodIPsIfPresent(pod)
	return util.RunAsHost(func() error {
		for _, pip := range podIPs {
			if uidMismatch, err := hostsideProbeSet.ClearEntriesWithIPAndComment(pip, podUID); err != nil {
				return err
			} else if uidMismatch != "" {
				log.Warnf("pod ip %s could not be removed from addressSet, found entry with pod UID %s instead", pip, uidMismatch)
			}
			log.Debugf("removed pod from host addressSet by ip %s", pip)
		}
		return nil
	})
}

func pruneHostAddrSet(expected sets.Set[netip.Addr], hostsideProbeSet set.AddressSetManager) error {
	return util.RunAsHost(func() error {
		actualIPSetContents, err := hostsideProbeSet.ListEntriesByIP()
		if err != nil {
			log.Warnf("unable to list addressSet: %v", err)
			return err
		}
		actual := sets.New(actualIPSetContents...)
		stales := actual.DifferenceInPlace(expected)

		for staleIP := range stales {
			if err := hostsideProbeSet.ClearEntriesWithIP(staleIP); err != nil {
				return err
			}
			log.Debugf("removed stale ip %s from host addressSet %s", staleIP, hostsideProbeSet.GetPrefix())
		}
		return nil
	})
}
