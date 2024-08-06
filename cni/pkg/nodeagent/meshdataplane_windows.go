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
	"net/netip"

	"istio.io/istio/cni/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type meshDataplane struct {
	kubeClient kubernetes.Interface
	netServer  MeshDataplane
}

func (s *meshDataplane) Start(ctx context.Context) {
	s.netServer.Start(ctx)
}

func (s *meshDataplane) Stop() {
	log.Info("CNI ambient server terminating, cleaning up node net rules")

	// TODO: Perform whatever cleanup we need to

	s.netServer.Stop()
}

func (s *meshDataplane) ConstructInitialSnapshot(ambientPods []*corev1.Pod) error {
	return s.netServer.ConstructInitialSnapshot(ambientPods)
}

func (s *meshDataplane) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	var retErr error
	err := s.netServer.AddPodToMesh(ctx, pod, podIPs, netNs)
	if err != nil {
		log.Errorf("failed to add pod to ztunnel: %v", err)
		if !errors.Is(err, ErrPartialAdd) {
			return err
		}
		retErr = err
	}

	log.Debugf("annotating pod %s", pod.Name)
	if err := util.AnnotateEnrolledPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		log.Errorf("failed to annotate pod enrollment: %v", err)
		retErr = err
	}

	// TODO: Windows-ify this
	// ipset is only relevant for pod healthchecks.
	// therefore, if we had *any* error adding the pod to the mesh
	// do not add the pod to the ipset, so that it will definitely *not* pass healthchecks,
	// and the operator can investigate.
	//
	// This is also important to avoid ipset sync issues if we add the pod ip to the ipset, but
	// enrolling fails because ztunnel (or the pod netns, or whatever) isn't ready yet,
	// and the pod is rescheduled with a new IP. In that case we don't get
	// a removal event, and so would never clean up the old IP that we eagerly-added.
	//
	// TODO one place this *can* fail is
	// - if a CNI plugin after us in the chain fails (currently, we are explicitly the last in the chain by design)
	// - the CmdAdd comes back thru here with a new IP
	// - we will never clean up that old IP that we "lost"
	// To fix this we probably need to impl CmdDel + manage our own podUID/IP mapping.
	if retErr == nil {
		// Handle node healthcheck probe rewrites
		// _, err = s.addPodToHostNSIpset(pod, podIPs)
		if err != nil {
			log.Errorf("failed to add pod to ipset: %s/%s %v", pod.Namespace, pod.Name, err)
			return err
		}
	} else {
		log.Errorf("pod: %s/%s was not enrolled and is unhealthy: %v", pod.Namespace, pod.Name, retErr)
	}

	return retErr
}

func (s *meshDataplane) RemovePodFromMesh(ctx context.Context, pod *corev1.Pod, isDelete bool) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)

	// Aggregate errors together, so that if part of the removal fails we still proceed with other steps.
	var errs []error

	// Remove the hostside ipset entry first, and unconditionally - if later failures happen, we never
	// want to leave stale entries
	// if err := removePodFromHostNSIpset(pod, &s.hostsideProbeIPSet); err != nil {
	// 	log.Errorf("failed to remove pod %s from host ipset, error was: %v", pod.Name, err)
	// 	errs = append(errs, err)
	// }

	// Specifically, at this time, we do _not_ want to un-annotate the pod if we cannot successfully remove it,
	// as the pod is probably in a broken state and we don't want to let go of it from the CP perspective while that is true.
	// So we will return if this fails.
	if err := s.netServer.RemovePodFromMesh(ctx, pod, isDelete); err != nil {
		log.Errorf("failed to remove pod from mesh: %v", err)
		errs = append(errs, err)
		return errors.Join(errs...)
	}

	// This should be the last step in all cases - once we do this, the CP will no longer consider this pod "ambient",
	// regardless of the state it is in
	log.Debug("removing annotation from pod")
	if err := util.AnnotateUnenrollPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		log.Errorf("failed to annotate pod unenrollment: %v", err)
		errs = append(errs, err)
		return errors.Join(errs...)
	}

	return errors.Join(errs...)
}
