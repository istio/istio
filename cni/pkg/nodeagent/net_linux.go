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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/cni/pkg/trafficmanager"
	"istio.io/istio/pkg/slices"
)

// Adapts CNI to ztunnel server. decoupled from k8s for easier integration testing.
type NetServer struct {
	ztunnelServer      ZtunnelServer
	currentPodSnapshot *podNetnsCache
	trafficManager     trafficmanager.TrafficRuleManager
	podNs              PodNetnsFinder
	// allow overriding for tests
	netnsRunner func(fdable NetnsFd, toRun func() error) error
}

var _ MeshDataplane = &NetServer{}

// ConstructInitialSnapshot is always called first, before Start.
// It takes a "snapshot" of ambient pods that were already running when the server started, and:
//
// - initializes an internal cache of pod info and netns handles with these existing pods.
// This cache will also be updated when the K8S informer gets a new pod.
// This cache represents the "state of the world" of all enrolled pods on the node this agent
// knows about, and will be sent to any connecting ztunnel as a startup message.
//
// - For each of these existing snapshotted pods, steps into their netNS, and reconciles their
// existing iptables rules against the expected set of rules. This is used to handle reconciling
// iptables rule drift/changes between versions.
func (s *NetServer) ConstructInitialSnapshot(existingAmbientPods []*corev1.Pod) error {
	var consErr []error

	podsByUID := slices.GroupUnique(existingAmbientPods, (*corev1.Pod).GetUID)
	if err := s.buildZtunnelSnapshot(podsByUID); err != nil {
		log.Warnf("failed to construct initial ztunnel snapshot: %v", err)
		consErr = append(consErr, err)
	}

	if s.trafficManager.ReconcileModeEnabled() {
		log.Info("inpod reconcile mode enabled")
		for _, pod := range existingAmbientPods {
			log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
			log.Debug("upgrading and reconciling inpod rules for already-running pod if necessary")
			err := s.reconcileExistingPod(pod)
			if err != nil {
				// for now, we will simply log an error, no need to append this error to the generic snapshot list
				log.Errorf("failed to reconcile inpod rules for pod, try restarting the pod, or removing and re-adding it to the mesh: %v", err)
			}
		}
	}
	return errors.Join(consErr...)
}

// Start starts the ztunnel connection listen server.
// ConstructInitialSnapshot should always be invoked before this function.
func (s *NetServer) Start(ctx context.Context) {
	log.Debug("starting ztunnel server")
	go s.ztunnelServer.Run(ctx)
}

// Stop stops the ztunnel connection listen server.
func (s *NetServer) Stop(_ bool) {
	log.Debug("stopping ztunnel server")
	s.ztunnelServer.Close()
}

// AddPodToMesh adds a pod to mesh by
// 1. Getting the netns (and making sure the netns is cached in the ztunnel state of the world snapshot)
// 2. Adding the pod's IPs to the hostnetns ipsets for node probe checks
// 3. Creating iptables rules inside the pod's netns
// 4. Notifying the connected ztunnel via GRPC to create a proxy for the pod
//
// You may ask why we pass the pod IPs separately from the pod manifest itself (which contains the pod IPs as a field)
// - this is because during add specifically, if CNI plugins have not finished executing,
// K8S may get a pod Add event without any IPs in the object, and the pod will later be updated with IPs.
//
// We always need the IPs, but this is fine because this AddPodToMesh can be called from the CNI plugin as well,
// which always has the firsthand info of the IPs, even before K8S does - so we pass them separately here because
// we actually may have them before K8S in the Pod object.
//
// Importantly, some of the failures that can occur when calling this function are retryable, and some are not.
// If this function returns a NonRetryableError, the function call should NOT be retried.
// Any other error indicates the function call can be retried.
func (s *NetServer) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	log.Info("adding pod to the mesh")
	// make sure the cache is aware of the pod, even if we don't have the netns yet.
	s.currentPodSnapshot.Ensure(string(pod.UID))
	openNetns, err := s.getOrOpenNetns(pod, netNs)
	if err != nil {
		// if we fail, we should not leave a dangling UID in the snapshot.
		s.currentPodSnapshot.Take(string(pod.UID))
		return NewErrNonRetryableAdd(err)
	}

	podCfg := getPodLevelTrafficOverrides(pod)

	log.Debug("calling CreateInpodRules")
	if err := s.netnsRunner(openNetns, func() error {
		return s.trafficManager.CreateInpodRules(log, podCfg)
	}); err != nil {
		// We currently treat any failure to create inpod rules as non-retryable/catastrophic,
		// and return a NonRetryableError in this case.
		log.Errorf("failed to update POD inpod: %s/%s %v", pod.Namespace, pod.Name, err)
		s.currentPodSnapshot.Take(string(pod.UID))
		return NewErrNonRetryableAdd(err)
	}

	// For *any* other failures after a successful `CreateInpodRules` call, we must return
	// the error as-is.
	//
	// This is so that if it is removed from the mesh, the inpod rules will unconditionally
	// be removed.
	//
	// Additionally, unlike the other errors, it is safe to retry regular errors with another
	// `AddPodToMesh`, in case a ztunnel connection later becomes available.
	log.Debug("notifying subscribed node proxies")
	if err := s.sendPodToZtunnelAndWaitForAck(ctx, pod, openNetns); err != nil {
		return err
	}
	return nil
}

// RemovePodFromMesh is called when a pod needs to be removed from the mesh.
//
// It:
// - Informs the connected ztunnel that the pod no longer needs to be proxied.
// - Removes the pod's netns file handle from the cache/state of the world snapshot.
// - Steps into the pod netns to remove the inpod iptables redirection rules.
//
// If this function returns an error, the removal may be retried.
func (s *NetServer) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	log.WithLabels("delete", isDelete).Debugf("removing pod from the mesh")

	// Whether pod is already deleted or not, we need to let go of our netns ref.
	openNetns := s.currentPodSnapshot.Take(string(pod.UID))
	if openNetns == nil {
		log.Debug("failed to find pod netns during removal")
	}

	// If the pod is already deleted or terminated, we do not need to clean up the pod network -- only the host side.
	if !isDelete {
		if openNetns != nil {
			// pod is removed from the mesh, but is still running. remove traffic rules
			log.Debugf("calling DeleteInpodRules")
			if err := s.netnsRunner(openNetns, func() error { return s.trafficManager.DeleteInpodRules(log) }); err != nil {
				return fmt.Errorf("failed to delete inpod rules: %w", err)
			}
		} else {
			log.Warn("pod netns already gone, not deleting inpod rules")
		}
	}

	log.Debug("removing pod from ztunnel")
	if err := s.ztunnelServer.PodDeleted(ctx, string(pod.UID)); err != nil {
		log.Errorf("failed to delete pod from ztunnel: %v", err)
		return err
	}
	return nil
}

func newNetServer(ztunnelServer ZtunnelServer, podNsMap *podNetnsCache, trafficManager trafficmanager.TrafficRuleManager, podNs PodNetnsFinder) *NetServer {
	return &NetServer{
		ztunnelServer:      ztunnelServer,
		currentPodSnapshot: podNsMap,
		podNs:              podNs,
		trafficManager:     trafficManager,
		netnsRunner:        NetnsDo,
	}
}

// reconcileExistingPod is intended to run on node agent startup, for each pod that was already enrolled prior to startup.
// Will reconcile any in-pod iptables rules the pod may already have against this node agent's expected/required in-pod iptables rules.
//
// This is used to handle upgrades and such. Note that this call should be idempotent for any pod already in the mesh,
// but should never need to be invoked outside of node agent startup.
func (s *NetServer) reconcileExistingPod(pod *corev1.Pod) error {
	openNetns, err := s.getNetns(pod)
	if err != nil {
		return err
	}

	podCfg := getPodLevelTrafficOverrides(pod)

	if err := s.netnsRunner(openNetns, func() error {
		return s.trafficManager.CreateInpodRules(log, podCfg)
	}); err != nil {
		return err
	}

	return nil
}

func (s *NetServer) rescanPod(pod *corev1.Pod) error {
	// this can happen if the pod was dynamically added to the mesh after it was created.
	// in that case, try finding the netns using procfs.
	filter := map[types.UID]*corev1.Pod{
		pod.UID: pod,
	}
	return s.scanProcForPodsAndCache(filter)
}

func (s *NetServer) getOrOpenNetns(pod *corev1.Pod, netNs string) (Netns, error) {
	if netNs == "" {
		return s.getNetns(pod)
	}
	return s.openNetns(pod, netNs)
}

func (s *NetServer) openNetns(pod *corev1.Pod, netNs string) (Netns, error) {
	return s.currentPodSnapshot.UpsertPodCache(pod, netNs)
}

func (s *NetServer) getNetns(pod *corev1.Pod) (Netns, error) {
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

func (s *NetServer) sendPodToZtunnelAndWaitForAck(ctx context.Context, pod *corev1.Pod, netns Netns) error {
	return s.ztunnelServer.PodAdded(ctx, pod, netns)
}

func (s *NetServer) buildZtunnelSnapshot(ambientPodUIDs map[types.UID]*corev1.Pod) error {
	// first add all the pods as empty:
	for uid := range ambientPodUIDs {
		s.currentPodSnapshot.Ensure(string(uid))
	}

	// populate full pod snapshot from cgroups
	return s.scanProcForPodsAndCache(ambientPodUIDs)
}

func (s *NetServer) scanProcForPodsAndCache(pods map[types.UID]*corev1.Pod) error {
	// TODO: maybe remove existing uids in s.currentPodSnapshot from the filter set.
	res, err := s.podNs.FindNetnsForPods(pods)
	if err != nil {
		return err
	}

	for uid, wl := range res {
		s.currentPodSnapshot.UpsertPodCacheWithNetns(uid, wl)
	}
	return nil
}
