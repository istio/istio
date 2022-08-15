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

	"go.uber.org/multierr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var repairLog = log.RegisterScope("repair", "CNI race condition repair", 0)

// The pod reconciler struct. Contains state used to reconcile broken pods.
type brokenPodReconciler struct {
	client client.Interface
	cfg    *config.RepairConfig
}

// Constructs a new brokenPodReconciler struct.
func newBrokenPodReconciler(client client.Interface, cfg *config.RepairConfig) brokenPodReconciler {
	return brokenPodReconciler{
		client: client,
		cfg:    cfg,
	}
}

func (bpr brokenPodReconciler) ReconcilePod(pod v1.Pod) (err error) {
	repairLog.Debugf("Reconciling pod %s", pod.Name)

	if bpr.cfg.DeletePods {
		err = multierr.Append(err, bpr.deleteBrokenPod(pod))
	} else if bpr.cfg.LabelPods {
		err = multierr.Append(err, bpr.labelBrokenPod(pod))
	}
	return err
}

// Label all pods detected as broken by ListPods with a customizable label
func (bpr brokenPodReconciler) LabelBrokenPods() (err error) {
	// Get a list of all broken pods
	podList, err := bpr.ListBrokenPods()
	if err != nil {
		return err
	}

	for _, pod := range podList.Items {
		err = multierr.Append(err, bpr.labelBrokenPod(pod))
	}
	return err
}

func (bpr brokenPodReconciler) labelBrokenPod(pod v1.Pod) (err error) {
	// Added for safety, to make sure no healthy pods get labeled.
	m := podsRepaired.With(typeLabel.Value(labelType))
	if !bpr.detectPod(pod) {
		m.With(resultLabel.Value(resultSkip)).Increment()
		return
	}
	repairLog.Infof("Pod detected as broken, adding label: %s/%s", pod.Namespace, pod.Name)

	labels := pod.GetLabels()
	if _, ok := labels[bpr.cfg.LabelKey]; ok {
		m.With(resultLabel.Value(resultSkip)).Increment()
		repairLog.Infof("Pod %s/%s already has label with key %s, skipping", pod.Namespace, pod.Name, bpr.cfg.LabelKey)
		return
	}

	if labels == nil {
		labels = map[string]string{}
	}

	repairLog.Infof("Labeling pod %s/%s with label %s=%s", pod.Namespace, pod.Name, bpr.cfg.LabelKey, bpr.cfg.LabelValue)
	labels[bpr.cfg.LabelKey] = bpr.cfg.LabelValue
	pod.SetLabels(labels)

	if _, err = bpr.client.CoreV1().Pods(pod.Namespace).Update(context.TODO(), &pod, metav1.UpdateOptions{}); err != nil {
		repairLog.Errorf("Failed to update pod: %s", err)
		m.With(resultLabel.Value(resultFail)).Increment()
		return
	}
	m.With(resultLabel.Value(resultSuccess)).Increment()
	return
}

// Delete all pods detected as broken by ListPods
func (bpr brokenPodReconciler) DeleteBrokenPods() error {
	// Get a list of all broken pods
	podList, err := bpr.ListBrokenPods()
	if err != nil {
		return err
	}

	var loopErrors []error
	for _, pod := range podList.Items {
		repairLog.Infof("Deleting broken pod: %s/%s", pod.Namespace, pod.Name)
		if err := bpr.deleteBrokenPod(pod); err != nil {
			loopErrors = append(loopErrors, err)
		}
	}
	if len(loopErrors) > 0 {
		lerr := loopErrors[0].Error()
		for _, err := range loopErrors[1:] {
			lerr = fmt.Sprintf("%s,%s", lerr, err.Error())
		}
		return fmt.Errorf("%s", lerr)
	}
	return nil
}

func (bpr brokenPodReconciler) deleteBrokenPod(pod v1.Pod) error {
	m := podsRepaired.With(typeLabel.Value(deleteType))
	// Added for safety, to make sure no healthy pods get labeled.
	if !bpr.detectPod(pod) {
		m.With(resultLabel.Value(resultSkip)).Increment()
		return nil
	}
	repairLog.Infof("Pod detected as broken, deleting: %s/%s", pod.Namespace, pod.Name)
	err := bpr.client.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
	if err != nil {
		m.With(resultLabel.Value(resultFail)).Increment()
		return err
	}
	m.With(resultLabel.Value(resultSuccess)).Increment()
	return nil
}

// Lists all pods identified as broken by our Filter criteria
func (bpr brokenPodReconciler) ListBrokenPods() (list v1.PodList, err error) {
	var rawList *v1.PodList
	rawList, err = bpr.client.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		LabelSelector: bpr.cfg.LabelSelectors,
		FieldSelector: bpr.cfg.FieldSelectors,
	})
	if err != nil {
		return
	}

	list.Items = []v1.Pod{}
	for _, pod := range rawList.Items {
		if bpr.detectPod(pod) {
			list.Items = append(list.Items, pod)
		}
	}

	return
}

// Given a pod, returns 'true' if the pod is a match to the brokenPodReconciler filter criteria.
func (bpr brokenPodReconciler) detectPod(pod v1.Pod) bool {
	// Helper function; checks that a container's termination message matches filter
	matchTerminationMessage := func(state *v1.ContainerStateTerminated) bool {
		// If we are filtering on init container termination message and the termination message of 'state' does not match, exit
		trimmedTerminationMessage := strings.TrimSpace(bpr.cfg.InitTerminationMsg)
		return trimmedTerminationMessage == "" || trimmedTerminationMessage == strings.TrimSpace(state.Message)
	}
	// Helper function; checks that container exit code matches filter
	matchExitCode := func(state *v1.ContainerStateTerminated) bool {
		// If we are filtering on init container exit code and the termination message does not match, exit
		if ec := bpr.cfg.InitExitCode; ec == 0 || ec == int(state.ExitCode) {
			return true
		}
		return false
	}

	// Only check pods that have the sidecar annotation; the rest can be
	// ignored.
	if bpr.cfg.SidecarAnnotation != "" {
		if _, ok := pod.ObjectMeta.Annotations[bpr.cfg.SidecarAnnotation]; !ok {
			return false
		}
	}

	// For each candidate pod, iterate across all init containers searching for
	// crashlooping init containers that match our criteria
	for _, container := range pod.Status.InitContainerStatuses {
		// Skip the container if the InitContainerName is not a match and our
		// InitContainerName filter is non-empty.
		if bpr.cfg.InitContainerName != "" && container.Name != bpr.cfg.InitContainerName {
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

func StartRepair(ctx context.Context, cfg *config.RepairConfig) {
	repairLog.Info("Start CNI race condition repair.")
	if !cfg.Enabled {
		repairLog.Info("CNI repair is disable.")
		return
	}

	clientSet, err := clientSetup()
	if err != nil {
		repairLog.Fatalf("CNI repair could not construct clientSet: %s", err)
	}

	podFixer := newBrokenPodReconciler(clientSet, cfg)

	if cfg.RunAsDaemon {
		rc, err := NewRepairController(podFixer)
		if err != nil {
			repairLog.Fatalf("Fatal error constructing repair controller: %+v", err)
		}
		rc.Run(ctx.Done())
	} else {
		err = nil
		if podFixer.cfg.LabelPods {
			err = multierr.Append(err, podFixer.LabelBrokenPods())
		}
		if podFixer.cfg.DeletePods {
			err = multierr.Append(err, podFixer.DeleteBrokenPods())
		}
		if err != nil {
			repairLog.Fatalf(err.Error())
		}
	}
}

// Set up Kubernetes client using kubeconfig (or in-cluster config if no file provided)
func clientSetup() (clientset *client.Clientset, err error) {
	config, err := kube.DefaultRestConfig("", "")
	if err != nil {
		return
	}
	clientset, err = client.NewForConfig(config)
	return
}
