//go:build windows
// +build windows

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

	"istio.io/istio/pkg/slices"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Adapts CNI to ztunnel server. decoupled from k8s for easier integration testing.
type NetServer struct {
	ztunnelServer      ZtunnelServer
	currentPodSnapshot *podNetnsCache
	// allow overriding for tests
	netnsRunner func(fdable NetnsFd, toRun func() error) error
}

var _ MeshDataplane = &NetServer{}

func (s *NetServer) Start(ctx context.Context) {
	log.Debug("starting ztunnel server")
	go s.ztunnelServer.Run(ctx)
}

func (s *NetServer) Stop(_ bool) {
	log.Debug("stopping ztunnel server")
	s.ztunnelServer.Close()
}

// ConstructInitialSnapshot takes a "snapshot" of current ambient pods and
//
// 1. Constructs a ztunnel state message to initialize ztunnel
// 2. Syncs the host ipset
func (s *NetServer) ConstructInitialSnapshot(ambientPods []*corev1.Pod) error {
	var consErr []error

	podsByUID := slices.GroupUnique(ambientPods, (*corev1.Pod).GetUID)
	if err := s.buildZtunnelSnapshot(podsByUID); err != nil {
		log.Warnf("failed to construct initial ztunnel snapshot: %v", err)
		consErr = append(consErr, err)
	}

	return errors.Join(consErr...)
}

func (s *NetServer) buildZtunnelSnapshot(ambientPodUIDs map[types.UID]*corev1.Pod) error {
	// first add all the pods as empty:
	for uid := range ambientPodUIDs {
		s.currentPodSnapshot.Ensure(string(uid))
	}

	// TODO: windows-ify
	return s.scanProcForPodsAndCache(ambientPodUIDs)
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
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	log.Infof("adding pod to the mesh")
	// make sure the cache is aware of the pod, even if we don't have the netns yet.
	s.currentPodSnapshot.Ensure(string(pod.UID))
	openNetns, err := s.getOrOpenNetns(pod, netNs)
	if err != nil {
		return err
	}

	log.Debug("calling CreateInpodRules")
	if err := s.netnsRunner(openNetns, func() error {
		return s.podIptables.CreateInpodRules(log, &HostProbeSNATIP, &HostProbeSNATIPV6)
	}); err != nil {
		log.Errorf("failed to update POD inpod: %s/%s %v", pod.Namespace, pod.Name, err)
		return err
	}

	// For *any* failures after calling `CreateInpodRules`, we must return PartialAdd error.
	// The pod was injected with iptables rules, so it must be annotated as "inpod" - even if
	// the following fails.
	// This is so that if it is removed from the mesh, the inpod rules will unconditionally
	// be removed.

	log.Debug("notifying subscribed node proxies")
	if err := s.sendPodToZtunnelAndWaitForAck(ctx, pod, openNetns); err != nil {
		return NewErrPartialAdd(err)
	}
	return nil
}

// RemovePodFromMesh is called when a pod needs to be removed from the mesh
func (s *NetServer) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	log.WithLabels("delete", isDelete).Debugf("removing pod from the mesh")

	// Aggregate errors together, so that if part of the cleanup fails we still proceed with other steps.
	var errs []error

	// Whether pod is already deleted or not, we need to let go of our netns ref.
	openNetns := s.currentPodSnapshot.Take(string(pod.UID))
	if openNetns == nil {
		log.Debug("failed to find pod netns during removal")
	}

	// If the pod is already deleted or terminated, we do not need to clean up the pod network -- only the host side.
	if !isDelete {
		if openNetns != nil {
			// pod is removed from the mesh, but is still running. remove iptables rules
			log.Debugf("calling DeleteInpodRules")
			if err := s.netnsRunner(openNetns, func() error { return s.podIptables.DeleteInpodRules() }); err != nil {
				return fmt.Errorf("failed to delete inpod rules: %w", err)
			}
		} else {
			log.Warn("pod netns already gone, not deleting inpod rules")
		}
	}

	log.Debug("removing pod from ztunnel")
	if err := s.ztunnelServer.PodDeleted(ctx, string(pod.UID)); err != nil {
		log.Errorf("failed to delete pod from ztunnel: %v", err)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// make sure uid is in the cache, even if we don't have a netns
func (p *podNetnsCache) Ensure(uid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.currentPodCache[uid]; !ok {
		p.currentPodCache[uid] = workloadInfo{}
	}
}
