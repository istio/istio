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

package reconciler

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/istio/mdp/controller/pkg/apis/mdp/v1alpha1"
	"istio.io/istio/mdp/controller/pkg/cluster"
	"istio.io/istio/mdp/controller/pkg/kubeclient"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/tools/bug-report/pkg/content"
	"istio.io/pkg/log"
)

// MDPReconciler reconciles MDP controlled resources.
type MDPReconciler struct {
	client         client.Client
	extendedClient kube.ExtendedClient
	restConfig     *rest.Config
	clientSet      *kubernetes.Clientset
	//mdpConfig      *v1alpha1.MDPConfig
	opts           *Options
	params         *content.Params
	backoff        BackoffCtrl
}

// Options are options for MDPReconciler.
type Options struct {
	// Log is a console logger for user visible CLI output.
	Log clog.Logger
}

const (
	istioProxyContainerName = "istio/proxyv2"
	maxPodsPerStep = 5
)

var (
	defaultOptions = &Options{
		Log: clog.NewDefaultLogger(),
	}
	scope = log.RegisterScope("mdp", "Managed Data Plane", 0)
)

// NewMDPReconciler creates a MDPReconciler and returns a ptr to it
func NewMDPReconciler(client client.Client, restConfig *rest.Config, opts *Options) (*MDPReconciler, error) {
	if opts == nil {
		opts = defaultOptions
	}

	clientConfig, clientSet, err := kubeclient.New("", "")
	if err != nil {
		return nil, err
	}
	extendedClient, err := kube.NewExtendedClient(clientConfig, "")
	if err != nil {
		return nil, err
	}
	params := &content.Params{
		Client: extendedClient,
	}
	return &MDPReconciler{
		client:         client,
		extendedClient: extendedClient,
		restConfig:     restConfig,
		clientSet:      clientSet,
		//mdpConfig:      mdpConfig,
		opts:           opts,
		params:         params,
		backoff:        BackoffCtrl{state: make(map[string]backoffCounter)},
	}, nil
}

// Reconcile reconciles the associated resources.
func (h *MDPReconciler) Reconcile(ctx context.Context, mdpConfig *v1alpha1.MDPConfig) (*v1alpha1.MDPStatus, error) {
	if mdpConfig == nil {
		// allows controller code to function for cases where spec is not provided.
		mdpConfig = &v1alpha1.MDPConfig{}
		mdpConfig.Spec = &v1alpha1.MDPConfigSpec{}
	}
	targetPct := float32(mdpConfig.Spec.ProxyTargetPct)
	newVersion := mdpConfig.Spec.ProxyVersion

	scope.Infof("New version: %s", newVersion)
	resources, err := cluster.GetClusterResources(context.Background(), h.clientSet)
	if err != nil {
		return nil, err
	}
	// TODO: handle this more intelligently.
	if len(resources.Pod) < 1000 {
		scope.Infof("Cluster resource tree:\n\n%s\n\n", resources)
	} else {
		scope.Debugf("Cluster resource tree:\n\n%s\n\n", resources)
	}

	elligiblePods := getElligiblePods(resources.Pod)

	currentPct := h.currentNewVersionPct(elligiblePods, newVersion)
	if currentPct >= targetPct {
		scope.Infof("Percentage of Pods at new version %s is %2.1f%%, meets target of %2.1f%%, not restarting.", newVersion, currentPct, targetPct)
		return &v1alpha1.MDPStatus{
			Status:    v1alpha1.MDPStatus_READY,
			//ProxyTargetVersionActualPct: currentPct,
		}, nil
	}
	scope.Infof("Percentage of Pods at new version %s is %2.1f%%, below target of %2.1f%%, restarting a pod.", newVersion, currentPct, targetPct)
	outStatus := &v1alpha1.MDPStatus{
		Status:    v1alpha1.MDPStatus_RECONCILING,
		//ProxyTargetVersionActualPct: currentPct,
	}
	// don't pick 5 pods if that would overshoot our target.
	totalDesiredRestarts := (targetPct - currentPct)*float32(len(elligiblePods))
	var podCount int
	if totalDesiredRestarts > maxPodsPerStep {
		podCount = maxPodsPerStep
	} else {
		podCount = int(totalDesiredRestarts)
	}
	// don't pick pods whose last call 423'd
	targetPods := pickPods(elligiblePods, podCount, newVersion, h.backoff.GetInelligiblePodNAmes())
	if len(targetPods) == 0 {
		outStatus.Status = v1alpha1.MDPStatus_ERROR
		return outStatus, fmt.Errorf("Too many pods can't restart due to PDB limits")
	}
	var outErr error
	for key, pod := range targetPods {
		scope.Infof("Restarting Pod %s/%s.", pod.Namespace, pod.Name)
		// For large clusters, we should restart multiple pods per reconcile loop using some heuristics.
		err = h.clientSet.CoreV1().Pods(pod.Namespace).Evict(ctx, &v1beta1.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
		})
		if err != nil {
			if errors.IsTooManyRequests(err) {
				// pods can't evict due to PDB, activate exponential backoff
				h.backoff.Fail(key)
			}
			// keep trying the others before exiting?
			outErr = multierror.Append(outErr, err)
		}
		h.backoff.Succeed(key)
	}
	h.backoff.Increment()
	return outStatus, outErr
}

func pickPods(pods map[string]*v1.Pod, count int, newVersion string, backedOffPodNames map[string]struct{}) map[string]*v1.Pod {
	out := make(map[string]*v1.Pod, 0)
	for key, pod := range pods {
		if _, ok := backedOffPodNames[key]; ok {
			continue
		}
		for _, container := range pod.Spec.Containers {
			ver, ok := proxyVersion(container.Image)
			if !ok {
				continue
			}
			if ver == newVersion {
				continue
			}
			out[key] = pod
			if len(out) >= count {
				return out
			}
			break
		}
	}
	return out
}

func getElligiblePods(pods map[string]*v1.Pod) map[string]*v1.Pod {
	// TODO: make this aware of channels/revisions when available
	out := make(map[string]*v1.Pod, 0)
	for key, pod := range pods {
		// check that controller type is supported.
		ctrl := metav1.GetControllerOf(pod)
		if ctrl == nil || ctrl.Kind != "ReplicaSet" {
			continue
		}
		if pod.Status.Phase != v1.PodRunning {
			continue
		}
		// TODO: maybe check which label controls this namespace, to be sure this will get new version?
		out[key] = pod
	}
	return out
}

// proxyVersion return true if the image string in an istio proxy and the version of the proxy, or false and an empty
// string otherwise.
func proxyVersion(image string) (string, bool) {
	vv := strings.Split(image, ":")
	if !strings.Contains(vv[0], istioProxyContainerName) {
		return "", false
	}
	if len(vv) == 1 {
		return "", true
	}
	return vv[1], true
}

func (h *MDPReconciler) currentNewVersionPct(pods map[string]*v1.Pod, newVersion string) float32 {
	totalPods, newPods := 0, 0

	for _, pod := range pods {
		scope.Infof("Checking pod %s", pod.Name)
		for _, container := range pod.Spec.Containers {
			version, ok := proxyVersion(container.Image)
			if !ok {
				continue
			}
			totalPods++
			scope.Infof("Pod %s/%s has proxy version %s", pod.Namespace, pod.Name, version)
			if version == newVersion {
				newPods++
			}
		}
	}

	if totalPods == 0 {
		// all of 0 pods are at the new version
		return 100.0
	}
	return 100.0 * float32(newPods) / float32(totalPods)
}

// SetStatusBegin updates the status field on the MDPConfig instance before reconciling.
func SetStatusBegin(mdpConfig *v1alpha1.MDPConfig, client client.Client) error {

	mdpConfigCheck := &v1alpha1.MDPConfig{}
	namespacedName := types.NamespacedName{
		Name:      mdpConfig.Name,
		Namespace: mdpConfig.Namespace,
	}
	if err := client.Get(context.TODO(), namespacedName, mdpConfigCheck); err != nil {
		if runtime.IsNotRegisteredError(err) {
			// CRD not yet installed in cluster, nothing to update.
			return nil
		}
		return fmt.Errorf("failed to get MDPConfig before updating status due to %v", err)
	}
	mdpConfigCheck.Status = &v1alpha1.MDPStatus{Status: v1alpha1.MDPStatus_RECONCILING}
	return client.Status().Update(context.TODO(), mdpConfigCheck)
}

// SetStatusComplete updates the status field on the MDPConfig instance.
func SetStatusComplete(mdpConfig *v1alpha1.MDPConfig, client client.Client, status *v1alpha1.MDPStatus) error {
	mdpConfigCheck := &v1alpha1.MDPConfig{}
	namespacedName := types.NamespacedName{
		Name:      mdpConfig.Name,
		Namespace: mdpConfig.Namespace,
	}
	if err := client.Get(context.TODO(), namespacedName, mdpConfigCheck); err != nil {
		return fmt.Errorf("failed to get MDPConfig before updating status due to %v", err)
	}
	mdpConfigCheck.Status = status
	return client.Status().Update(context.TODO(), mdpConfigCheck)
}

// getClient returns the kubernetes client associated with this MDPReconciler
func (h *MDPReconciler) getClient() client.Client {
	return h.client
}

type BackoffCtrl struct {
	state map[string]backoffCounter
}

type backoffCounter struct {
	// cooldown is the duration of the last backoff
	cooldown int
	// iter is how far into the cooldown we are
	iter int
}

func (b *BackoffCtrl) Fail(podKey string) {
	if counter, ok := b.state[podKey]; ok {
		counter.cooldown *= 2
		counter.iter = 0
	}
	b.state[podKey] = backoffCounter{2, 0}
}

func (b *BackoffCtrl) Increment() {
	for _, v := range b.state {
		v.iter ++
	}
}

func (b *BackoffCtrl) Succeed(podKey string) {
	delete(b.state, podKey)
}

func (b *BackoffCtrl) GetInelligiblePodNAmes() map[string]struct{} {
	out := make(map[string]struct{})
	for k, v := range b.state {
		if v.iter < v.cooldown {
			out[k] = struct{}{}
		}
	}
	return out
}
