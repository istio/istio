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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// repairPod actually dynamically repairs a pod. This is done by entering the pods network namespace and setting up rules.
// This differs from the general CNI plugin flow, which triggers before the pod fully starts.
// Additionally, we need to jump through hoops to find the network namespace.
func (c *Controller) repairPod(pod *corev1.Pod) error {
	m := podsRepaired.With(typeLabel.Value(repairType))
	log := repairLog.WithLabels("pod", pod.Namespace+"/"+pod.Name)
	key := types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}
	// We will get an event every time the pod changes. The repair is not instantaneous, though -- it will only recover
	// once the pod restarts (in CrashLoopBackoff), which can take some time.
	// We don't want to constantly try to apply the iptables rules, which is unneeded and will fail.
	// Instead, we track which UIDs we repaired and skip them if already repaired.
	//
	// An alternative would be to write something to the Pod (status, annotation, etc).
	// However, this requires elevated privileges we want to avoid
	if uid, f := c.repairedPods[key]; f {
		if uid == pod.UID {
			log.Debugf("Skipping pod, already repaired")
		} else {
			// This is unexpected, bubble up to an error. Might be missing event, or invalid assumption in our code.
			// Either way, we will skip.
			log.Errorf("Skipping pod, already repaired with an unexpected UID %v vs %v", uid, pod.UID)
		}
		return nil
	}
	log.Infof("Repairing pod...")

	// Fetch the pod's network namespace. This must run in the host process due to how the procfs /ns/net works.
	// This will get a network namespace ID. This ID is scoped to the network namespace we running in.
	// As such, we need to be in the host namespace: the CNI pod namespace has no relation to the users pod namespace.
	netns, err := runInHost(func() (string, error) { return getPodNetNs(pod) })
	if err != nil {
		m.With(resultLabel.Value(resultFail)).Increment()
		return fmt.Errorf("get netns: %v", err)
	}
	log = log.WithLabels("netns", netns)

	redirector := redirectRunningPod
	if c.cfg.NativeNftables {
		redirector = redirectRunningPodNFT
	}

	if err := redirector(pod, netns); err != nil {
		log.Errorf("failed to setup redirection: %v", err)
		m.With(resultLabel.Value(resultFail)).Increment()
		return err
	}

	c.repairedPods[key] = pod.UID
	log.Infof("pod repaired")
	m.With(resultLabel.Value(resultSuccess)).Increment()
	return nil
}
