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

func (s *meshDataplane) Stop(skipCleanup bool) {
	log.Info("CNI ambient server terminating, cleaning up node net rules")

	// TODO: Perform whatever cleanup we need to

	s.netServer.Stop(skipCleanup)
}

func (s *meshDataplane) ConstructInitialSnapshot(ambientPods []*corev1.Pod) error {
	return s.netServer.ConstructInitialSnapshot(ambientPods)
}

// TODO: Add doc comment
func (s *meshDataplane) AddPodToMesh(ctx context.Context, pod *corev1.Pod, podIPs []netip.Addr, netNs string) error {
	log := log.WithLabels("ns", pod.Namespace, "name", pod.Name)
	err := s.netServer.AddPodToMesh(ctx, pod, podIPs, netNs)
	if err != nil {
		log.Errorf("failed to add pod to ztunnel: %v", err)
		if !errors.Is(err, ErrNonRetryableAdd) {
			return err
		}

		log.Error("failed to add pod to ztunnel: pod partially added, annotating with pending status")
		if err := util.AnnotatePartiallyEnrolledPod(s.kubeClient, &pod.ObjectMeta); err != nil {
			// If we have an error annotating the partial status - that is itself retryable.
			return err
		}

		// Otherwise return the original error
		return err
	}

	// TODO: Windows-ify this (probably using ebpf)
	// if _, err := s.addPodToHostNSIpset(pod, podIPs); err != nil {
	// 	log.Errorf("failed to add pod to ipset, pod will fail healthchecks: %v", err)
	// 	// Adding pod to ipset should always be an upsert, so should not fail
	// 	// unless we have a kernel incompatibility - thus it should either
	// 	// never fail, or isn't usefully retryable.
	// 	// For now tho, err on the side of being loud in the logs,
	// 	// since retrying in that case isn't _harmful_ and means all pods will fail anyway.
	// 	return err
	// }

	log.Debugf("annotating pod %s", pod.Name)
	if err := util.AnnotateEnrolledPod(s.kubeClient, &pod.ObjectMeta); err != nil {
		log.Errorf("failed to annotate pod enrollment: %v", err)
	}

	return nil
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
