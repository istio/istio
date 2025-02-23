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
	"encoding/json"
	"fmt"

	"github.com/Microsoft/hcsshim/hcn"
	podsandbox "github.com/containerd/containerd/pkg/cri/sbserver/podsandbox"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	criapi "k8s.io/cri-api/pkg/apis"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/util/sets"
)

type PodNetNsHNSFinder struct {
	criClient criapi.RuntimeService
}

var _ PodNetnsFinder = &PodNetNsHNSFinder{}

func NewPodNetNsHNSFinder(crlClient criapi.RuntimeService) *PodNetNsHNSFinder {
	return &PodNetNsHNSFinder{
		criClient: crlClient,
	}
}

func (p *PodNetNsHNSFinder) FindNetnsForPods(pods map[types.UID]*corev1.Pod) (PodToNetns, error) {
	/*
		for each pod sandbox, check the status to find its sandbox id
		if we have already seen its sandbox, skip it
		if we haven't seen the sandbox, check the process cgroup and see if we
		can extract a pod uid from it.
		if we can, open the netns, and save a map of uid->netns-fd
	*/

	podUIDNetns := make(PodToNetns)
	sandboxes, err := p.criClient.ListPodSandbox(context.Background(), nil)
	if err != nil {
		return nil, err
	}

	desiredUIDs := sets.New(maps.Keys(pods)...)
	for _, sandbox := range sandboxes {
		// Query sandbox status. We ask for verbose output so we can get the network namespace
		// later
		resp, err := p.criClient.PodSandboxStatus(context.Background(), sandbox.Id, true)
		if err != nil {
			// Can't break yet, but log the error
			log.Warnf("error requesting sandbox status for %s: %v", sandbox.GetId(), err)
			continue
		}
		status := resp.GetStatus()
		podUID := types.UID(status.GetMetadata().GetUid())
		if !desiredUIDs.Contains(podUID) {
			// We don't care about this one
			continue
		}

		if _, ok := podUIDNetns[string(podUID)]; ok {
			// TODO: examine if there's a case where this actually happens
			// We've already seen this pod UID; shouldn't happen so just skip for now
			log.Debugf("already seen sandbox for pod %s. Skipping...", podUID)
			continue
		}

		// We've got the correct sandbox information, now we need to get the namespace
		// guid
		var sandbox podsandbox.SandboxInfo
		err = json.Unmarshal([]byte(resp.Info["info"]), &sandbox)
		if err != nil {
			log.Warnf("error unmarshalling sandbox info for %s: %v. Info was %#v", podUID, err, resp.Info)
			continue
		}
		nsGuid := sandbox.RuntimeSpec.Windows.Network.NetworkNamespace
		// Ok guid retrieved, but it would be nice to go ahead and get the id
		// let's talk to HNS and get that
		ns, err := hcn.GetNamespaceByID(nsGuid)
		if err != nil {
			log.Warnf("error getting namespace for %s: %v", podUID, err)
		}
		// Open the namespace so we have access to the handle
		pod := pods[podUID]
		// Get endpoint ids from the namespace
		endpointIDs, err := getNamespaceEndpoints(ns.Resources)
		if err != nil {
			return nil, fmt.Errorf("could not get namespace endpoints: %w", err)
		}
		podUIDNetns[string(podUID)] = &workloadInfo{
			workload: podToWorkload(pod),
			namespace: &namespaceCloser{
				ns: WindowsNamespace{
					ID:          ns.NamespaceId,
					GUID:        nsGuid,
					EndpointIds: endpointIDs,
				},
			},
		}
	}

	return podUIDNetns, nil
}
