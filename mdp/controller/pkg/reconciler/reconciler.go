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
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
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
	mdpConfig      *v1alpha1.MDPConfig
	opts           *Options
	params         *content.Params
}

// Options are options for MDPReconciler.
type Options struct {
	// Log is a console logger for user visible CLI output.
	Log clog.Logger
}

const (
	istioProxyContainerName = "istio/proxyv2"
)

var (
	defaultOptions = &Options{
		Log: clog.NewDefaultLogger(),
	}
	scope = log.RegisterScope("mdp", "Managed Data Plane", 0)
)

// NewMDPReconciler creates a MDPReconciler and returns a ptr to it
func NewMDPReconciler(client client.Client, restConfig *rest.Config, mdpConfig *v1alpha1.MDPConfig, opts *Options) (*MDPReconciler, error) {
	if opts == nil {
		opts = defaultOptions
	}
	if mdpConfig == nil {
		// allows controller code to function for cases where spec is not provided.
		mdpConfig = &v1alpha1.MDPConfig{}
		mdpConfig.Spec = &v1alpha1.MDPConfigSpec{}
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
		mdpConfig:      mdpConfig,
		opts:           opts,
		params:         params,
	}, nil
}

// Reconcile reconciles the associated resources.
// TODO: make this more efficient by using an informer to track restart rather than requeuing requests.
func (h *MDPReconciler) Reconcile() (*v1alpha1.MDPStatus, error) {
	targetPct := h.mdpConfig.Spec.ProxyTargetPct
	newVersion := h.mdpConfig.Spec.ProxyVersion

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

	currentPct := h.currentNewVersionPct(resources, newVersion)
	if currentPct >= targetPct {
		scope.Infof("Percentage of Pods at new version %s is %2.1f%%, meets target of %2.1f%%, not restarting.", newVersion, currentPct, targetPct)
		return nil, nil
	}
	scope.Infof("Percentage of Pods at new version %s is %2.1f%%, below target of %2.1f%%, restarting a pod.", newVersion, currentPct, targetPct)
	for _, pod := range resources.Pod {
		scope.Infof("Checking pod %s", pod.Name)
		for _, container := range pod.Spec.Containers {
			ver, ok := proxyVersion(container.Image)
			if !ok {
				continue
			}
			if ver != newVersion {
				// TODO: use eviction API here to restart. Only log for now.
				// For large clusters, we should restart multiple pods per reconcile loop using some heuristics.
				scope.Infof("Restarting Pod %s/%s.", pod.Namespace, pod.Name)
				return nil, nil
			}
		}
	}

	return nil, nil
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

func (h *MDPReconciler) currentNewVersionPct(resources *cluster.Resources, newVersion string) float32 {
	totalPods, newPods := 0, 0

	for _, pod := range resources.Pod {
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
func (h *MDPReconciler) SetStatusBegin() error {
	mdpConfig := &v1alpha1.MDPConfig{}
	namespacedName := types.NamespacedName{
		Name:      h.mdpConfig.Name,
		Namespace: h.mdpConfig.Namespace,
	}
	if err := h.getClient().Get(context.TODO(), namespacedName, mdpConfig); err != nil {
		if runtime.IsNotRegisteredError(err) {
			// CRD not yet installed in cluster, nothing to update.
			return nil
		}
		return fmt.Errorf("failed to get MDPConfig before updating status due to %v", err)
	}
	mdpConfig.Status = &v1alpha1.MDPStatus{Status: v1alpha1.MDPStatus_RECONCILING}
	return h.getClient().Status().Update(context.TODO(), mdpConfig)
}

// SetStatusComplete updates the status field on the MDPConfig instance.
func (h *MDPReconciler) SetStatusComplete(status *v1alpha1.MDPStatus) error {
	mdpConfig := &v1alpha1.MDPConfig{}
	namespacedName := types.NamespacedName{
		Name:      h.mdpConfig.Name,
		Namespace: h.mdpConfig.Namespace,
	}
	if err := h.getClient().Get(context.TODO(), namespacedName, mdpConfig); err != nil {
		return fmt.Errorf("failed to get MDPConfig before updating status due to %v", err)
	}
	mdpConfig.Status = status
	return h.getClient().Status().Update(context.TODO(), mdpConfig)
}

// getCRName returns the name of the CR associated with h.
func (h *MDPReconciler) getCRName() (string, error) {
	if h.mdpConfig == nil {
		return "", nil
	}
	objAccessor, err := meta.Accessor(h.mdpConfig)
	if err != nil {
		return "", err
	}
	return objAccessor.GetName(), nil
}

// getCRNamespace returns the namespace of the CR associated with h.
func (h *MDPReconciler) getCRNamespace() (string, error) {
	if h.mdpConfig == nil {
		return "", nil
	}
	objAccessor, err := meta.Accessor(h.mdpConfig)
	if err != nil {
		return "", err
	}
	return objAccessor.GetNamespace(), nil
}

// getClient returns the kubernetes client associated with this MDPReconciler
func (h *MDPReconciler) getClient() client.Client {
	return h.client
}
