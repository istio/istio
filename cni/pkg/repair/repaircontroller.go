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
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/plugin"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
)

type Controller struct {
	client       kube.Client
	pods         kclient.Client[*corev1.Pod]
	queue        controllers.Queue
	cfg          config.RepairConfig
	events       kclient.EventRecorder
	repairedPods map[types.NamespacedName]types.UID
}

func NewRepairController(client kube.Client, cfg config.RepairConfig) (*Controller, error) {
	c := &Controller{
		cfg:          cfg,
		client:       client,
		events:       kclient.NewEventRecorder(client, "cni-repair"),
		repairedPods: map[types.NamespacedName]types.UID{},
	}
	fieldSelectors := []string{}
	if cfg.FieldSelectors != "" {
		fieldSelectors = append(fieldSelectors, cfg.FieldSelectors)
	}
	// filter out pod events from different nodes
	fieldSelectors = append(fieldSelectors, fmt.Sprintf("spec.nodeName=%v", cfg.NodeName))
	c.pods = kclient.NewFiltered[*corev1.Pod](client, kclient.Filter{
		LabelSelector: cfg.LabelSelectors,
		FieldSelector: strings.Join(fieldSelectors, ","),
	})
	c.queue = controllers.NewQueue("repair pods",
		controllers.WithReconciler(c.Reconcile),
		controllers.WithMaxAttempts(5))
	c.pods.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))

	return c, nil
}

func (c *Controller) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync("repair controller", stop, c.pods.HasSynced)
	c.queue.Run(stop)
	c.pods.ShutdownHandlers()
}

func (c *Controller) Reconcile(key types.NamespacedName) error {
	pod := c.pods.Get(key.Name, key.Namespace)
	if pod == nil {
		delete(c.repairedPods, key) // Ensure we do not leak
		// Pod deleted, nothing to do
		return nil
	}
	return c.ReconcilePod(pod)
}

func (c *Controller) ReconcilePod(pod *corev1.Pod) (err error) {
	if !c.matchesFilter(pod) {
		return // Skip, pod doesn't need repair
	}
	repairLog.Debugf("Reconciling pod %s", pod.Name)

	if c.cfg.RepairPods {
		return c.repairPod(pod)
	} else if c.cfg.DeletePods {
		return c.deleteBrokenPod(pod)
	} else if c.cfg.LabelPods {
		return c.labelBrokenPod(pod)
	}
	return nil
}

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

	if err := redirectRunningPod(pod, netns); err != nil {
		log.Errorf("failed to setup redirection: %v", err)
		m.With(resultLabel.Value(resultFail)).Increment()
		return err
	}
	c.repairedPods[key] = pod.UID
	log.Infof("pod repaired")
	m.With(resultLabel.Value(resultSuccess)).Increment()
	return nil
}

// redirectRunningPod dynamically enters the provided pod, that is already running, and programs it's networking configuration.
func redirectRunningPod(pod *corev1.Pod, netns string) error {
	pi := plugin.ExtractPodInfo(pod)
	redirect, err := plugin.NewRedirect(pi)
	if err != nil {
		return fmt.Errorf("setup redirect: %v", err)
	}
	rulesMgr := plugin.IptablesInterceptRuleMgr()
	if err := rulesMgr.Program(pod.Name, netns, redirect); err != nil {
		return fmt.Errorf("program redirection: %v", err)
	}
	return nil
}

const (
	ReasonDeleteBrokenPod = "DeleteBrokenPod"
	ReasonLabelBrokenPod  = "LabelBrokenPod"
)

func (c *Controller) deleteBrokenPod(pod *corev1.Pod) error {
	m := podsRepaired.With(typeLabel.Value(deleteType))
	repairLog.Infof("Pod detected as broken, deleting: %s/%s", pod.Namespace, pod.Name)

	// Make sure we are deleting what we think we are...
	preconditions := &metav1.Preconditions{
		UID:             &pod.UID,
		ResourceVersion: &pod.ResourceVersion,
	}
	err := c.client.Kube().CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{
		Preconditions: preconditions,
	})
	if err != nil {
		c.events.Write(pod, corev1.EventTypeWarning, ReasonDeleteBrokenPod, "pod detected as broken, but failed to delete: %v", err)
		m.With(resultLabel.Value(resultFail)).Increment()
		return err
	}
	c.events.Write(pod, corev1.EventTypeWarning, ReasonDeleteBrokenPod, "pod detected as broken, deleted")
	m.With(resultLabel.Value(resultSuccess)).Increment()
	return nil
}

func (c *Controller) labelBrokenPod(pod *corev1.Pod) error {
	// Added for safety, to make sure no healthy pods get labeled.
	m := podsRepaired.With(typeLabel.Value(labelType))
	repairLog.Infof("Pod detected as broken, adding label: %s/%s", pod.Namespace, pod.Name)

	labels := pod.GetLabels()
	if _, ok := labels[c.cfg.LabelKey]; ok {
		m.With(resultLabel.Value(resultSkip)).Increment()
		repairLog.Infof("Pod %s/%s already has label with key %s, skipping", pod.Namespace, pod.Name, c.cfg.LabelKey)
		return nil
	}

	repairLog.Infof("Labeling pod %s/%s with label %s=%s", pod.Namespace, pod.Name, c.cfg.LabelKey, c.cfg.LabelValue)

	patchBytes := fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, c.cfg.LabelKey, c.cfg.LabelValue)
	// Both "pods" and "pods/status" can mutate the metadata. However, pods/status is lower privilege, so we use that instead.
	_, err := c.client.Kube().CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name, types.MergePatchType,
		[]byte(patchBytes), metav1.PatchOptions{}, "status")
	if err != nil {
		repairLog.Errorf("Failed to update pod: %s", err)
		c.events.Write(pod, corev1.EventTypeWarning, ReasonLabelBrokenPod, "pod detected as broken, but failed to label: %v", err)
		m.With(resultLabel.Value(resultFail)).Increment()
		return err
	}
	c.events.Write(pod, corev1.EventTypeWarning, ReasonLabelBrokenPod, "pod detected as broken, labeled")
	m.With(resultLabel.Value(resultSuccess)).Increment()
	return nil
}

// MatchesFilter returns true if the pod matches the repair filter criteria
func (c *Controller) matchesFilter(pod *corev1.Pod) bool {
	// Helper function; checks that a container's termination message matches filter
	matchTerminationMessage := func(state *corev1.ContainerStateTerminated) bool {
		// If we are filtering on init container termination message and the termination message of 'state' does not match, exit
		trimmedTerminationMessage := strings.TrimSpace(c.cfg.InitTerminationMsg)
		return trimmedTerminationMessage == "" || trimmedTerminationMessage == strings.TrimSpace(state.Message)
	}
	// Helper function; checks that container exit code matches filter
	matchExitCode := func(state *corev1.ContainerStateTerminated) bool {
		// If we are filtering on init container exit code and the termination message does not match, exit
		if ec := c.cfg.InitExitCode; ec == 0 || ec == int(state.ExitCode) {
			return true
		}
		return false
	}

	// Only check pods that have the sidecar annotation; the rest can be
	// ignored.
	if c.cfg.SidecarAnnotation != "" {
		if _, ok := pod.ObjectMeta.Annotations[c.cfg.SidecarAnnotation]; !ok {
			return false
		}
	}

	// For each candidate pod, iterate across all init containers searching for
	// crashlooping init containers that match our criteria
	for _, container := range pod.Status.InitContainerStatuses {
		// Skip the container if the InitContainerName is not a match and our
		// InitContainerName filter is non-empty.
		if c.cfg.InitContainerName != "" && container.Name != c.cfg.InitContainerName {
			continue
		}

		// For safety, check the containers *current* status. If the container
		// successfully exited, we NEVER want to identify this pod as broken.
		// If the pod is going to fail, the failure state will show up in
		// LastTerminationState eventually.
		if state := container.State.Terminated; state != nil {
			if state.Reason == "Completed" || state.ExitCode == 0 {
				continue
			}
		}

		// Check the LastTerminationState struct for information about why the container
		// last exited. If a pod is using the CNI configuration check init container,
		// it will start crashlooping and populate this struct.
		if state := container.LastTerminationState.Terminated; state != nil {
			// Verify the container state matches our filter criteria
			if matchTerminationMessage(state) && matchExitCode(state) {
				return true
			}
		}
	}
	return false
}
