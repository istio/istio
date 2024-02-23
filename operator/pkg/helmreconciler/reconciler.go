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

package helmreconciler

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/api/operator/v1alpha1"
	revtag "istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/istioctl/pkg/util/formatting"
	istioV1Alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/metrics"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/webhook"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/version"
)

// HelmReconciler reconciles resources rendered by a set of helm charts.
type HelmReconciler struct {
	client     client.Client
	kubeClient kube.Client
	iop        *istioV1Alpha1.IstioOperator
	opts       *Options
	// copy of the last generated manifests.
	manifests name.ManifestMap
	// dependencyWaitCh is a map of signaling channels. A parent with children ch1...chN will signal
	// dependencyWaitCh[ch1]...dependencyWaitCh[chN] when it's completely installed.
	dependencyWaitCh map[name.ComponentName]chan struct{}

	// The fields below are for metrics and reporting
	countLock     *sync.Mutex
	prunedKindSet map[schema.GroupKind]struct{}
}

// Options are options for HelmReconciler.
type Options struct {
	// DryRun executes all actions but does not write anything to the cluster.
	DryRun bool
	// Log is a console logger for user visible CLI output.
	Log clog.Logger
	// Wait determines if we will wait for resources to be fully applied. Only applies to components that have no
	// dependencies.
	Wait bool
	// WaitTimeout controls the amount of time to wait for resources in a component to become ready before giving up.
	WaitTimeout time.Duration
	// Log tracks the installation progress for all components.
	ProgressLog *progress.Log
	// Force ignores validation errors
	Force bool
	// SkipPrune will skip pruning
	SkipPrune bool
}

var defaultOptions = &Options{
	Log:         clog.NewDefaultLogger(),
	ProgressLog: progress.NewLog(),
}

// NewHelmReconciler creates a HelmReconciler and returns a ptr to it
func NewHelmReconciler(client client.Client, kubeClient kube.Client, iop *istioV1Alpha1.IstioOperator, opts *Options) (*HelmReconciler, error) {
	if opts == nil {
		opts = defaultOptions
	}
	if opts.ProgressLog == nil {
		opts.ProgressLog = progress.NewLog()
	}
	if int64(opts.WaitTimeout) == 0 {
		if waitForResourcesTimeoutStr, found := os.LookupEnv("WAIT_FOR_RESOURCES_TIMEOUT"); found {
			if waitForResourcesTimeout, err := time.ParseDuration(waitForResourcesTimeoutStr); err == nil {
				opts.WaitTimeout = waitForResourcesTimeout
			} else {
				scope.Warnf("invalid env variable value: %s for 'WAIT_FOR_RESOURCES_TIMEOUT'! falling back to default value...", waitForResourcesTimeoutStr)
				// fallback to default wait resource timeout
				opts.WaitTimeout = defaultWaitResourceTimeout
			}
		} else {
			// fallback to default wait resource timeout
			opts.WaitTimeout = defaultWaitResourceTimeout
		}
	}
	if iop == nil {
		// allows controller code to function for cases where IOP is not provided (e.g. operator remove).
		iop = &istioV1Alpha1.IstioOperator{}
		iop.Spec = &v1alpha1.IstioOperatorSpec{}
	}
	return &HelmReconciler{
		client:           client,
		kubeClient:       kubeClient,
		iop:              iop,
		opts:             opts,
		dependencyWaitCh: initDependencies(),
		countLock:        &sync.Mutex{},
		prunedKindSet:    make(map[schema.GroupKind]struct{}),
	}, nil
}

// initDependencies initializes the dependencies channel tree.
func initDependencies() map[name.ComponentName]chan struct{} {
	ret := make(map[name.ComponentName]chan struct{})
	for _, parent := range ComponentDependencies {
		for _, child := range parent {
			ret[child] = make(chan struct{}, 1)
		}
	}
	return ret
}

// Reconcile reconciles the associated resources.
func (h *HelmReconciler) Reconcile() (*v1alpha1.InstallStatus, error) {
	if err := util.CreateNamespace(h.kubeClient.Kube(), istioV1Alpha1.Namespace(h.iop.Spec), h.networkName(), h.opts.DryRun); err != nil {
		return nil, err
	}
	manifestMap, err := h.RenderCharts()
	if err != nil {
		return nil, err
	}

	err = h.analyzeWebhooks(manifestMap[name.PilotComponentName])
	if err != nil {
		if h.opts.Force {
			scope.Error("invalid webhook configs; continuing because of --force")
		} else {
			return nil, err
		}
	}
	status := h.processRecursive(manifestMap)

	var pruneErr error
	if !h.opts.SkipPrune && !h.opts.DryRun {
		h.opts.ProgressLog.SetState(progress.StatePruning)
		pruneErr = h.Prune(manifestMap, false)
		h.reportPrunedObjectKind()
	}
	return status, pruneErr
}

// processRecursive processes the given manifests in an order of dependencies defined in h. Dependencies are a tree,
// where a child must wait for the parent to complete before starting.
func (h *HelmReconciler) processRecursive(manifests name.ManifestMap) *v1alpha1.InstallStatus {
	componentStatus := make(map[string]*v1alpha1.InstallStatus_VersionStatus)

	// mu protects the shared InstallStatus componentStatus across goroutines
	var mu sync.Mutex
	// wg waits for all manifest processing goroutines to finish
	var wg sync.WaitGroup

	for c, ms := range manifests {
		c, ms := c, ms
		wg.Add(1)
		go func() {
			var appliedResult AppliedResult
			defer wg.Done()
			if s := h.dependencyWaitCh[c]; s != nil {
				scope.Infof("%s is waiting on dependency...", c)
				<-s
				scope.Infof("Dependency for %s has completed, proceeding.", c)
			}

			// Possible paths for status are RECONCILING -> {NONE, ERROR, HEALTHY}. NONE means component has no resources.
			// In NONE case, the component is not shown in overall status.
			mu.Lock()
			setStatus(componentStatus, c, v1alpha1.InstallStatus_RECONCILING, nil)
			mu.Unlock()

			status := v1alpha1.InstallStatus_NONE
			var err error
			if len(ms) != 0 {
				m := name.Manifest{
					Name:    c,
					Content: name.MergeManifestSlices(ms),
				}
				appliedResult, err = h.ApplyManifest(m)
				if err != nil {
					status = v1alpha1.InstallStatus_ERROR
				} else if appliedResult.Succeed() {
					status = v1alpha1.InstallStatus_HEALTHY
				}
			}

			mu.Lock()
			setStatus(componentStatus, c, status, err)
			mu.Unlock()

			// Signal all the components that depend on us.
			for _, ch := range ComponentDependencies[c] {
				scope.Infof("Unblocking dependency %s.", ch)
				h.dependencyWaitCh[ch] <- struct{}{}
			}
		}()
	}
	wg.Wait()

	metrics.ReportOwnedResourceCounts()

	out := &v1alpha1.InstallStatus{
		Status:          overallStatus(componentStatus),
		ComponentStatus: componentStatus,
	}

	return out
}

// Delete resources associated with the custom resource instance
func (h *HelmReconciler) Delete() error {
	defer func() {
		metrics.ReportOwnedResourceCounts()
		h.reportPrunedObjectKind()
	}()
	iop := h.iop
	if iop.Spec.Revision == "" {
		err := h.Prune(nil, true)
		return err
	}
	// Delete IOP with revision:
	// for this case we update the status field to pending if there are still proxies pointing to this revision
	// and we do not prune shared resources, same effect as `istioctl uninstall --revision foo` command.
	status, err := h.PruneControlPlaneByRevisionWithController(iop.Spec)
	if err != nil {
		return err
	}

	// check status here because terminating iop's status can't be updated.
	if status.Status == v1alpha1.InstallStatus_ACTION_REQUIRED {
		return fmt.Errorf("action is required before deleting the iop instance: %s", status.Message)
	}

	// updating status taking no effect for terminating resources.
	if err := h.SetStatusComplete(status); err != nil {
		return err
	}
	return nil
}

func (h *HelmReconciler) DeleteIOPInClusterIfExists(iop *istioV1Alpha1.IstioOperator) {
	// Delete the previous IstioOperator CR if it exists.
	objectKey := client.ObjectKeyFromObject(iop)
	receiver := &unstructured.Unstructured{}
	receiver.SetGroupVersionKind(istioV1Alpha1.IstioOperatorGVK)
	if err := h.client.Get(context.TODO(), objectKey, receiver); err == nil {
		_ = h.client.Delete(context.TODO(), receiver)
	}
}

// SetStatusBegin updates the status field on the IstioOperator instance before reconciling.
func (h *HelmReconciler) SetStatusBegin() error {
	isop := &istioV1Alpha1.IstioOperator{}
	if err := h.getClient().Get(context.TODO(), config.NamespacedName(h.iop), isop); err != nil {
		if runtime.IsNotRegisteredError(err) {
			// CRD not yet installed in cluster, nothing to update.
			return nil
		}
		return fmt.Errorf("failed to get IstioOperator before updating status due to %v", err)
	}
	if isop.Status == nil {
		isop.Status = &v1alpha1.InstallStatus{Status: v1alpha1.InstallStatus_RECONCILING}
	} else {
		cs := isop.Status.ComponentStatus
		for cn := range cs {
			cs[cn] = &v1alpha1.InstallStatus_VersionStatus{
				Status: v1alpha1.InstallStatus_RECONCILING,
			}
		}
		isop.Status.Status = v1alpha1.InstallStatus_RECONCILING
	}
	return h.getClient().Status().Update(context.TODO(), isop)
}

// SetStatusComplete updates the status field on the IstioOperator instance based on the resulting err parameter.
func (h *HelmReconciler) SetStatusComplete(status *v1alpha1.InstallStatus) error {
	iop := &istioV1Alpha1.IstioOperator{}
	if err := h.getClient().Get(context.TODO(), config.NamespacedName(h.iop), iop); err != nil {
		return fmt.Errorf("failed to get IstioOperator before updating status due to %v", err)
	}
	iop.Status = status
	return h.getClient().Status().Update(context.TODO(), iop)
}

// setStatus sets the status for the component with the given name, which is a key in the given map.
// If the status is InstallStatus_NONE, the component name is deleted from the map.
// Otherwise, if the map key/value is missing, one is created.
func setStatus(s map[string]*v1alpha1.InstallStatus_VersionStatus, componentName name.ComponentName, status v1alpha1.InstallStatus_Status, err error) {
	cn := string(componentName)
	if status == v1alpha1.InstallStatus_NONE {
		delete(s, cn)
		return
	}
	if _, ok := s[cn]; !ok {
		s[cn] = &v1alpha1.InstallStatus_VersionStatus{}
	}
	s[cn].Status = status
	if err != nil {
		s[cn].Error = err.Error()
	}
}

// overallStatus returns the summary status over all components.
// - If all components are HEALTHY, overall status is HEALTHY.
// - If one or more components are RECONCILING and others are HEALTHY, overall status is RECONCILING.
// - If one or more components are UPDATING and others are HEALTHY, overall status is UPDATING.
// - If components are a mix of RECONCILING, UPDATING and HEALTHY, overall status is UPDATING.
// - If any component is in ERROR state, overall status is ERROR.
func overallStatus(componentStatus map[string]*v1alpha1.InstallStatus_VersionStatus) v1alpha1.InstallStatus_Status {
	ret := v1alpha1.InstallStatus_HEALTHY
	for _, cs := range componentStatus {
		if cs.Status == v1alpha1.InstallStatus_ERROR {
			ret = v1alpha1.InstallStatus_ERROR
			break
		} else if cs.Status == v1alpha1.InstallStatus_UPDATING {
			ret = v1alpha1.InstallStatus_UPDATING
			break
		} else if cs.Status == v1alpha1.InstallStatus_RECONCILING {
			ret = v1alpha1.InstallStatus_RECONCILING
			break
		}
	}
	return ret
}

// getCoreOwnerLabels returns a map of labels for associating installation resources. This is the common
// labels shared between all resources; see getOwnerLabels to get labels per-component labels
func (h *HelmReconciler) getCoreOwnerLabels() (map[string]string, error) {
	crName, err := h.getCRName()
	if err != nil {
		return nil, err
	}
	crNamespace, err := h.getCRNamespace()
	if err != nil {
		return nil, err
	}
	labels := make(map[string]string)

	labels[operatorLabelStr] = operatorReconcileStr
	if crName != "" {
		labels[OwningResourceName] = crName
	}
	if crNamespace != "" {
		labels[OwningResourceNamespace] = crNamespace
	}
	labels[istioVersionLabelStr] = version.Info.Version

	revision := ""
	if h.iop != nil {
		revision = h.iop.Spec.Revision
	}
	if revision == "" {
		revision = "default"
	}
	labels[label.IoIstioRev.Name] = revision

	return labels, nil
}

func (h *HelmReconciler) addComponentLabels(coreLabels map[string]string, componentName string) map[string]string {
	labels := map[string]string{}
	for k, v := range coreLabels {
		labels[k] = v
	}

	labels[IstioComponentLabelStr] = componentName

	return labels
}

// getOwnerLabels returns a map of labels for the given component name, revision and owning CR resource name.
func (h *HelmReconciler) getOwnerLabels(componentName string) (map[string]string, error) {
	labels, err := h.getCoreOwnerLabels()
	if err != nil {
		return nil, err
	}

	return h.addComponentLabels(labels, componentName), nil
}

// applyLabelsAndAnnotations applies owner labels and annotations to the object.
func (h *HelmReconciler) applyLabelsAndAnnotations(obj runtime.Object, componentName string) error {
	labels, err := h.getOwnerLabels(componentName)
	if err != nil {
		return err
	}

	for k, v := range labels {
		err := util.SetLabel(obj, k, v)
		if err != nil {
			return err
		}
	}
	return nil
}

// getCRName returns the name of the CR associated with h.
func (h *HelmReconciler) getCRName() (string, error) {
	if h.iop == nil {
		return "", nil
	}
	objAccessor, err := meta.Accessor(h.iop)
	if err != nil {
		return "", err
	}
	return objAccessor.GetName(), nil
}

// getCRHash returns the cluster unique hash of the CR associated with h.
func (h *HelmReconciler) getCRHash(componentName string) (string, error) {
	crName, err := h.getCRName()
	if err != nil {
		return "", err
	}
	crNamespace, err := h.getCRNamespace()
	if err != nil {
		return "", err
	}
	var host string
	if h.kubeClient != nil && h.kubeClient.RESTConfig() != nil {
		host = h.kubeClient.RESTConfig().Host
	}
	return strings.Join([]string{crName, crNamespace, componentName, host}, "-"), nil
}

// getCRNamespace returns the namespace of the CR associated with h.
func (h *HelmReconciler) getCRNamespace() (string, error) {
	if h.iop == nil {
		return "", nil
	}
	objAccessor, err := meta.Accessor(h.iop)
	if err != nil {
		return "", err
	}
	return objAccessor.GetNamespace(), nil
}

// getClient returns the kubernetes client associated with this HelmReconciler
func (h *HelmReconciler) getClient() client.Client {
	return h.client
}

func (h *HelmReconciler) addPrunedKind(gk schema.GroupKind) {
	h.countLock.Lock()
	defer h.countLock.Unlock()
	h.prunedKindSet[gk] = struct{}{}
}

func (h *HelmReconciler) reportPrunedObjectKind() {
	h.countLock.Lock()
	defer h.countLock.Unlock()
	for gvk := range h.prunedKindSet {
		metrics.ResourcePruneTotal.
			With(metrics.ResourceKindLabel.Value(util.GKString(gvk))).
			Increment()
	}
}

func (h *HelmReconciler) analyzeWebhooks(whs []string) error {
	if len(whs) == 0 {
		return nil
	}

	// Add webhook manifests to be applied
	var localWebhookYAMLReaders []local.ReaderSource
	var parsedK8sObjects object.K8sObjects
	exists := revtag.PreviousInstallExists(context.Background(), h.kubeClient.Kube())
	for i, wh := range whs {
		k8sObjects, err := object.ParseK8sObjectsFromYAMLManifest(wh)
		if err != nil {
			return err
		}
		objYaml, err := k8sObjects.YAMLManifest()
		if err != nil {
			return err
		}
		// Here if we need to create a default tag, we need to skip the webhooks that are going to be deactivated.
		if !DetectIfTagWebhookIsNeeded(h.iop, exists) {
			whReaderSource := local.ReaderSource{
				Name:   fmt.Sprintf("installed-webhook-%d", i),
				Reader: strings.NewReader(objYaml),
			}
			localWebhookYAMLReaders = append(localWebhookYAMLReaders, whReaderSource)
		}
		parsedK8sObjects = append(parsedK8sObjects, k8sObjects...)
	}

	sa := local.NewSourceAnalyzer(analysis.Combine("webhook", &webhook.Analyzer{
		SkipServiceCheck:             true,
		SkipDefaultRevisionedWebhook: DetectIfTagWebhookIsNeeded(h.iop, exists),
	}), resource.Namespace(h.iop.Spec.GetNamespace()), resource.Namespace(istioV1Alpha1.Namespace(h.iop.Spec)), nil)

	// Add in-cluster webhooks
	objects := &unstructured.UnstructuredList{}
	objects.SetGroupVersionKind(gvk.MutatingWebhookConfiguration.Kubernetes())
	err := h.client.List(context.Background(), objects, &client.ListOptions{})
	if err != nil {
		return err
	}
	for i, obj := range objects.Items {
		objYAML, err := object.NewK8sObject(&obj, nil, nil).YAML()
		if err != nil {
			return err
		}
		whReaderSource := local.ReaderSource{
			Name:   fmt.Sprintf("in-cluster-webhook-%d", i),
			Reader: strings.NewReader(string(objYAML)),
		}
		err = sa.AddReaderKubeSource([]local.ReaderSource{whReaderSource})
		if err != nil {
			return err
		}
	}

	err = sa.AddReaderKubeSource(localWebhookYAMLReaders)
	if err != nil {
		return err
	}

	// Analyze webhooks
	res, err := sa.Analyze(make(chan struct{}))
	if err != nil {
		return err
	}
	relevantMessages := filterOutBasedOnResources(res.Messages, parsedK8sObjects)
	if len(relevantMessages) > 0 {
		o, err := formatting.Print(relevantMessages, formatting.LogFormat, false)
		if err != nil {
			return err
		}
		return fmt.Errorf("creating default tag would conflict:\n%v", o)
	}
	return nil
}

func filterOutBasedOnResources(ms diag.Messages, resources object.K8sObjects) diag.Messages {
	outputMessages := diag.Messages{}
	for _, m := range ms {
		for _, rs := range resources {
			if rs.Name == m.Resource.Metadata.FullName.Name.String() {
				outputMessages = append(outputMessages, m)
				break
			}
		}
	}
	return outputMessages
}

func (h *HelmReconciler) networkName() string {
	if h.iop.Spec.GetValues() == nil {
		return ""
	}
	globalI := h.iop.Spec.Values.AsMap()["global"]
	global, ok := globalI.(map[string]any)
	if !ok {
		return ""
	}
	nw, ok := global["network"].(string)
	if !ok {
		return ""
	}
	return nw
}

type ProcessDefaultWebhookOptions struct {
	Namespace string
	DryRun    bool
}

func DetectIfTagWebhookIsNeeded(iop *istioV1Alpha1.IstioOperator, exists bool) bool {
	rev := iop.Spec.Revision
	isDefaultInstallation := rev == "" && iop.Spec.Components.Pilot != nil && iop.Spec.Components.Pilot.Enabled.Value
	operatorManageWebhooks := operatorManageWebhooks(iop)
	return !operatorManageWebhooks && (!exists || isDefaultInstallation)
}

func ProcessDefaultWebhook(client kube.Client, iop *istioV1Alpha1.IstioOperator, exists bool, opt *ProcessDefaultWebhookOptions) (processed bool, err error) {
	// Detect whether previous installation exists prior to performing the installation.
	if !DetectIfTagWebhookIsNeeded(iop, exists) {
		return false, nil
	}
	rev := iop.Spec.Revision
	if rev == "" {
		rev = revtag.DefaultRevisionName
	}
	autoInjectNamespaces := validateEnableNamespacesByDefault(iop)

	ignorePruneLabel := map[string]string{
		OwningResourceNotPruned: "true",
	}

	o := &revtag.GenerateOptions{
		Tag:                  revtag.DefaultRevisionName,
		Revision:             rev,
		Overwrite:            true,
		AutoInjectNamespaces: autoInjectNamespaces,
		CustomLabels:         ignorePruneLabel,
		Generate:             opt.DryRun,
	}
	// If tag cannot be created could be remote cluster install, don't fail out.
	tagManifests, err := revtag.Generate(context.Background(), client, o, opt.Namespace)
	if err == nil && !opt.DryRun {
		if err = applyManifests(client, tagManifests); err != nil {
			return false, err
		}
	}
	return true, nil
}

func applyManifests(kubeClient kube.Client, manifests string) error {
	yamls := strings.Split(manifests, helm.YAMLSeparator)
	for _, yml := range yamls {
		if strings.TrimSpace(yml) == "" {
			continue
		}

		obj := &unstructured.Unstructured{}

		if err := yaml.Unmarshal([]byte(yml), obj); err != nil {
			return fmt.Errorf("failed to unmarshal YAML: %w", err)
		}
		var ogvr schema.GroupVersionResource
		if obj.GetKind() == name.MutatingWebhookConfigurationStr {
			ogvr = gvr.MutatingWebhookConfiguration
		} else if obj.GetKind() == name.ValidatingWebhookConfigurationStr {
			ogvr = gvr.ValidatingWebhookConfiguration
		}

		t := true
		_, err := kubeClient.Dynamic().Resource(ogvr).Namespace(obj.GetNamespace()).Patch(
			context.TODO(), obj.GetName(), types.ApplyPatchType, []byte(yml), metav1.PatchOptions{
				Force:        &t,
				FieldManager: fieldOwnerOperator,
			})
		if err != nil {
			return fmt.Errorf("failed to apply YAML: %w", err)
		}
	}
	return nil
}

// operatorManageWebhooks returns .Values.global.operatorManageWebhooks from the Istio Operator.
func operatorManageWebhooks(iop *istioV1Alpha1.IstioOperator) bool {
	if iop.Spec.GetValues() == nil {
		return false
	}
	globalValues := iop.Spec.Values.AsMap()["global"]
	global, ok := globalValues.(map[string]any)
	if !ok {
		return false
	}
	omw, ok := global["operatorManageWebhooks"].(bool)
	if !ok {
		return false
	}
	return omw
}

// validateEnableNamespacesByDefault checks whether there is .Values.sidecarInjectorWebhook.enableNamespacesByDefault set in the Istio Operator.
// Should be used in installer when deciding whether to enable an automatic sidecar injection in all namespaces.
func validateEnableNamespacesByDefault(iop *istioV1Alpha1.IstioOperator) bool {
	if iop == nil || iop.Spec == nil || iop.Spec.Values == nil {
		return false
	}
	sidecarValues := iop.Spec.Values.AsMap()["sidecarInjectorWebhook"]
	sidecarMap, ok := sidecarValues.(map[string]any)
	if !ok {
		return false
	}
	autoInjectNamespaces, ok := sidecarMap["enableNamespacesByDefault"].(bool)
	if !ok {
		return false
	}

	return autoInjectNamespaces
}
