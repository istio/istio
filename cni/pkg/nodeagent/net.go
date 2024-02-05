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
	"errors"
	"fmt"
	"net/netip"

	"golang.org/x/sys/unix"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/iptables"
	"istio.io/istio/cni/pkg/util"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

var log = istiolog.RegisterScope("ambient", "ambient controller")

// Adapts CNI to ztunnel server. decoupled from k8s for easier integration testing.
type NetServer struct {
	ztunnelServer        ZtunnelServer
	currentPodSnapshot   *podNetnsCache
	iptablesConfigurator *iptables.IptablesConfigurator
	podNs                PodNetnsFinder
	// allow overriding for tests
	netnsRunner        func(fdable NetnsFd, toRun func() error) error
	hostsideProbeIPSet ipset.IPSet
}

var _ MeshDataplane = &NetServer{}

func newNetServer(ztunnelServer ZtunnelServer, podNsMap *podNetnsCache,
	iptablesConfigurator *iptables.IptablesConfigurator, podNs PodNetnsFinder,
	probeSet ipset.IPSet,
) *NetServer {
	return &NetServer{
		ztunnelServer:        ztunnelServer,
		currentPodSnapshot:   podNsMap,
		podNs:                podNs,
		iptablesConfigurator: iptablesConfigurator,
		netnsRunner:          NetnsDo,
		hostsideProbeIPSet:   probeSet,
	}
}

func (s *NetServer) Start(ctx context.Context) {
	log.Debug("starting ztunnel server")
	go s.ztunnelServer.Run(ctx)
}

func (s *NetServer) Stop() {
	log.Debug("removing host iptables rules")
	s.iptablesConfigurator.DeleteHostRules()

	log.Debug("destroying host ipset")
	s.hostsideProbeIPSet.Flush()
	if err := s.hostsideProbeIPSet.DestroySet(); err != nil {
		log.Warnf("could not destroy host ipset on shutdown")
	}
	log.Debug("stopping ztunnel server")
	s.ztunnelServer.Close()
}

func (s *NetServer) rescanPod(pod *metav1.ObjectMeta) error {
	// this can happen if the pod was dynamically added to the mesh after it was created.
	// in that case, try finding the netns using procfs.
	filter := sets.New[types.UID]()
	filter.Insert(pod.UID)
	return s.scanProcForPodsAndCache(filter)
}

func (s *NetServer) getOrOpenNetns(pod *metav1.ObjectMeta, netNs string) (Netns, error) {
	if netNs == "" {
		return s.getNetns(pod)
	}
	return s.openNetns(pod, netNs)
}

func (s *NetServer) openNetns(pod *metav1.ObjectMeta, netNs string) (Netns, error) {
	return s.currentPodSnapshot.UpsertPodCache(string(pod.UID), netNs)
}

func (s *NetServer) getNetns(pod *metav1.ObjectMeta) (Netns, error) {
	openNetns := s.currentPodSnapshot.Get(string(pod.UID))
	if openNetns != nil {
		return openNetns, nil
	}
	log.Debug("pod netns was not found, trying to find it using procfs")
	// this can happen if the pod was dynamically added to the mesh after it was created.
	// in that case, try finding the netns using procfs.
	if err := s.rescanPod(pod); err != nil {
		log.Errorf("error scanning proc: error was %s", err)
		return nil, err
	}
	// try again. we can still get here if the pod is in the process of being created.
	// in this case the CNI will be invoked soon and provide us with the netns.
	openNetns = s.currentPodSnapshot.Get(string(pod.UID))
	if openNetns == nil {
		return nil, fmt.Errorf("can't find netns for pod, this is ok if this is a newly created pod (%w)", ErrPodNotFound)
	}

	return openNetns, nil
}

// AddPodToMesh adds a pod to mesh by
// 1. Getting the netns
// 2. Adding the pod's IPs to the hostnetns ipsets for node probe checks
// 3. Creating iptables rules inside the pod's netns
// 4. Notifying ztunnel via GRPC to create a proxy for the pod
//
// You may ask why we pass the pod IPs separately from the pod manifest itself (which contains the pod IPs as a field)
// - this is because during add specifically, if CNI plugins have not finished executing,
// K8S may get a pod Add event without any IPs in the object, and the pod will later be updated with IPs.
//
// We always need the IPs, but this is fine because this AddPodToMesh can be called from the CNI plugin as well,
// which always has the firsthand info of the IPs, even before K8S does - so we pass them separately here because
// we actually may have them before K8S in the Pod object.
func (s *NetServer) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	log.Infof("in pod mode - adding pod %s/%s to ztunnel ", pod.Namespace, pod.Name)
	// make sure the cache is aware of the pod, even if we don't have the netns yet.
	s.currentPodSnapshot.Ensure(string(pod.UID))
	openNetns, err := s.getOrOpenNetns(&pod.ObjectMeta, netNs)
	if err != nil {
		return err
	}

	// Handle node healthcheck probe rewrites
	err = addPodToHostNSIpset(pod, podIPs, &s.hostsideProbeIPSet)
	if err != nil {
		log.Errorf("failed to add pod to ipset: %s/%s %v", pod.Namespace, pod.Name, err)
		return err
	}

	log.Debug("calling CreateInpodRules")
	if err := s.netnsRunner(openNetns, func() error {
		return s.iptablesConfigurator.CreateInpodRules(&HostProbeSNATIP)
	}); err != nil {
		log.Errorf("failed to update POD inpod: %s/%s %v", pod.Namespace, pod.Name, err)
		return err
	}

	log.Debug("notifying subscribed node proxies")
	if err := s.sendPodToZtunnelAndWaitForAck(ctx, &pod.ObjectMeta, openNetns); err != nil {
		// we must return PartialAdd error here. the pod was injected with iptables rules,
		// so it should be annotated, so if it is removed from the mesh, the rules will be removed.
		// alternatively, we may not return an error at all, but we want this to fail on tests.
		return NewErrPartialAdd(err)
	}
	return nil
}

func (s *NetServer) sendPodToZtunnelAndWaitForAck(ctx context.Context, pod *metav1.ObjectMeta, netns Netns) error {
	return s.ztunnelServer.PodAdded(ctx, string(pod.UID), netns)
}

// ConstructInitialSnapshot takes a "snapshot" of current ambient pods and
//
// 1. Constructs a ztunnel state message to initialize ztunnel
// 2. Syncs the host ipset
func (s *NetServer) ConstructInitialSnapshot(ambientPods []*corev1.Pod) error {
	var consErr []error

	if err := s.syncHostIPSets(ambientPods); err != nil {
		log.Warnf("failed to sync host IPset: %v", err)
		consErr = append(consErr, err)
	}

	if err := s.buildZtunnelSnapshot(util.GetUniquePodUIDs(ambientPods)); err != nil {
		log.Warnf("failed to construct initial ztunnel snapshot: %v", err)
		consErr = append(consErr, err)
	}

	return errors.Join(consErr...)
}

func (s *NetServer) buildZtunnelSnapshot(ambientPodUIDs sets.Set[types.UID]) error {
	// first add all the pods as empty:
	for uid := range ambientPodUIDs {
		s.currentPodSnapshot.Ensure(string(uid))
	}

	// populate full pod snapshot from cgroups
	return s.scanProcForPodsAndCache(ambientPodUIDs)
}

func (s *NetServer) scanProcForPodsAndCache(filter sets.Set[types.UID]) error {
	// TODO: maybe remove existing uids in s.currentPodSnapshot from the filter set.
	res, err := s.podNs.FindNetnsForPods(filter)
	if err != nil {
		return err
	}

	for uid, netns := range res {
		s.currentPodSnapshot.UpsertPodCacheWithNetns(string(uid), netns)
	}
	return nil
}

func realDependencies() *dep.RealDependencies {
	return &dep.RealDependencies{
		CNIMode:          false, // we are in cni, but as we do the netns ourselves, we should keep this as false.
		NetworkNamespace: "",
	}
}

// Remove pod from mesh: pod is not deleted, we just want to remove it from the mesh.
func (s *NetServer) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	log.Debugf("Pod is now opt out... cleaning up.")

	openNetns := s.currentPodSnapshot.Take(string(pod.UID))
	if openNetns == nil {
		log.Warn("failed to find pod netns")
		return fmt.Errorf("failed to find pod netns")
	}
	// pod is removed from the mesh, but is still running. remove iptables rules
	log.Debugf("calling DeleteInpodRules.")
	if err := s.netnsRunner(openNetns, func() error { return s.iptablesConfigurator.DeleteInpodRules() }); err != nil {
		log.Errorf("failed to delete inpod rules %v", err)
		return fmt.Errorf("failed to delete inpod rules %w", err)
	}

	if err := removePodFromHostNSIpset(pod, &s.hostsideProbeIPSet); err != nil {
		log.Errorf("failed to remove pod %s from host ipset, error was: %v", pod.Name, err)
		return err
	}

	log.Debug("in pod mode - removing pod from ztunnel")
	if err := s.ztunnelServer.PodDeleted(ctx, string(pod.UID)); err != nil {
		log.Errorf("failed to delete pod from ztunnel: %v", err)
	}
	return nil
}

// Delete pod from mesh: pod is deleted. iptables rules will die with it, we just need to update ztunnel
func (s *NetServer) DelPodFromMesh(ctx context.Context, pod *corev1.Pod) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	log.Debug("Pod is now stopped... cleaning up.")

	if err := removePodFromHostNSIpset(pod, &s.hostsideProbeIPSet); err != nil {
		log.Errorf("failed to remove pod %s from host ipset, error was: %v", pod.Name, err)
		return err
	}

	log.Info("in pod mode - deleting pod from ztunnel")

	// pod is deleted, clean-up its open netns
	openNetns := s.currentPodSnapshot.Take(string(pod.UID))
	if openNetns == nil {
		log.Warn("failed to find pod netns")
	}

	if err := s.ztunnelServer.PodDeleted(ctx, string(pod.UID)); err != nil {
		return err
	}
	return nil
}

func (s *NetServer) syncHostIPSets(ambientPods []*corev1.Pod) error {
	var addedIPSnapshot []netip.Addr

	for _, pod := range ambientPods {
		podIPs := util.GetPodIPsIfPresent(pod)
		if len(podIPs) == 0 {
			log.Warnf("pod %s does not appear to have any assigned IPs, not syncing with ipset", pod.Name)
		} else {
			err := addPodToHostNSIpset(pod, podIPs, &s.hostsideProbeIPSet)
			if err != nil {
				return err
			}
			addedIPSnapshot = append(addedIPSnapshot, podIPs...)
		}

	}
	return pruneHostIPset(sets.New(addedIPSnapshot...), &s.hostsideProbeIPSet)
}

// addPodToHostNSIpset:
// 1. get pod manifest
// 2. Get all pod ips (might be several, v6/v4)
// 3. update ipsets accordingly
func addPodToHostNSIpset(pod *corev1.Pod, podIPs []netip.Addr, hostsideProbeSet *ipset.IPSet) error {
	// Add the pod UID as an ipset entry comment, so we can (more) easily find and delete
	// all relevant entries for a pod later.
	podUID := string(pod.ObjectMeta.UID)
	ipProto := uint8(unix.IPPROTO_TCP)

	var ipsetAddrErrs []error

	// For each pod IP
	for _, pip := range podIPs {
		// Add to host ipset
		log.Debugf("adding pod %s probe to ipset %s with ip %s", pod.Name, hostsideProbeSet.Name, pip)
		// Add IP/port combo to set. Note that we set Replace to true - a pod ip/port combo already being
		// in the set is perfectly fine, and something we can always safely overwrite, so we will.
		if err := hostsideProbeSet.AddIP(pip, ipProto, podUID, true); err != nil {
			ipsetAddrErrs = append(ipsetAddrErrs, err)
			log.Warnf("failed adding pod %s to ipset %s with ip %s, error was %s",
				pod.Name, hostsideProbeSet.Name, pip, err)
		}
	}

	return errors.Join(ipsetAddrErrs...)
}

func removePodFromHostNSIpset(pod *corev1.Pod, hostsideProbeSet *ipset.IPSet) error {
	podIPs := util.GetPodIPsIfPresent(pod)
	for _, pip := range podIPs {
		if err := hostsideProbeSet.ClearEntriesWithIP(pip); err != nil {
			return err
		}
		log.Debugf("removed pod name %s with UID %s from host ipset %s by ip %s", pod.Name, pod.UID, hostsideProbeSet.Name, pip)
	}

	return nil
}

func pruneHostIPset(expected sets.Set[netip.Addr], hostsideProbeSet *ipset.IPSet) error {
	actualIPSetContents, err := hostsideProbeSet.ListEntriesByIP()
	if err != nil {
		log.Warnf("unable to list IPSet: %v", err)
		return err
	}
	actual := sets.New[netip.Addr]()
	for _, ip := range actualIPSetContents {
		actual.Insert(ip)
	}

	stales := actual.Difference(expected)

	for staleIP := range stales {
		if err := hostsideProbeSet.ClearEntriesWithIP(staleIP); err != nil {
			return err
		}
		log.Debugf("removed stale ip %s from host ipset %s", staleIP, hostsideProbeSet.Name)
	}
	return nil
}
