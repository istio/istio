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

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/util/sets"
)

// DeploymentController implements a controller that materializes a Gateway into an in cluster gateway proxy
// to serve requests from. This is implemented with a Deployment and Service today.
// The implementation makes a few non-obvious choices - namely using Server Side Apply from go templates
// and not using controller-runtime.
//
// controller-runtime has a number of constraints that make it inappropriate for usage here, despite this
// seeming to be the bread and butter of the library:
// * It is not readily possible to bring existing Informers, which would require extra watches (#1668)
// * Goroutine leaks (#1655)
// * Excessive API-server calls at startup which have no benefit to us (#1603)
// * Hard to use with SSA (#1669)
// While these can be worked around, at some point it isn't worth the effort.
//
// Server Side Apply with go templates is an odd choice (no one likes YAML templating...) but is one of the few
// remaining options after all others are ruled out.
//   - Merge patch/Update cannot be used. If we always enforce that our object is *exactly* the same as
//     the in-cluster object we will get in endless loops due to other controllers that like to add annotations, etc.
//     If we chose to allow any unknown fields, then we would never be able to remove fields we added, as
//     we cannot tell if we created it or someone else did. SSA fixes these issues
//   - SSA using client-go Apply libraries is almost a good choice, but most third-party clients (Istio, MCS, and gateway-api)
//     do not provide these libraries.
//   - SSA using standard API types doesn't work well either: https://github.com/kubernetes-sigs/controller-runtime/issues/1669
//   - This leaves YAML templates, converted to unstructured types and Applied with the dynamic client.
type DeploymentController struct {
	client         kube.Client
	clusterID      cluster.ID
	env            *model.Environment
	queue          controllers.Queue
	patcher        patcher
	gateways       kclient.Client[*gateway.Gateway]
	gatewayClasses kclient.Client[*gateway.GatewayClass]

	clients         map[schema.GroupVersionResource]getter
	injectConfig    func() inject.WebhookConfig
	deployments     kclient.Client[*appsv1.Deployment]
	services        kclient.Client[*corev1.Service]
	serviceAccounts kclient.Client[*corev1.ServiceAccount]
	namespaces      kclient.Client[*corev1.Namespace]
	tagWatcher      revisions.TagWatcher
	revision        string
}

// Patcher is a function that abstracts patching logic. This is largely because client-go fakes do not handle patching
type patcher func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error

// classInfo holds information about a gateway class
type classInfo struct {
	// controller name for this class
	controller string
	// description for this class
	description string
	// The key in the templates to use for this class
	templates string
	// reportGatewayClassStatus, if enabled, will update the status when it is first created.
	reportGatewayClassStatus bool
}

var classInfos = getClassInfos()

func getClassInfos() map[string]classInfo {
	m := map[string]classInfo{
		defaultClassName: {
			controller:  constants.ManagedGatewayController,
			description: "The default Istio GatewayClass",
			templates:   "kube-gateway",
		},
	}
	if features.EnableAmbientControllers {
		m[constants.WaypointGatewayClassName] = classInfo{
			controller:               constants.ManagedGatewayMeshController,
			description:              "The default Istio waypoint GatewayClass",
			templates:                "waypoint",
			reportGatewayClassStatus: true,
		}
	}
	return m
}

var knownControllers = func() sets.String {
	res := sets.New[string]()
	for _, v := range classInfos {
		res.Insert(v.controller)
	}
	return res
}()

// NewDeploymentController constructs a DeploymentController and registers required informers.
// The controller will not start until Run() is called.
func NewDeploymentController(client kube.Client, clusterID cluster.ID, env *model.Environment,
	webhookConfig func() inject.WebhookConfig, injectionHandler func(fn func()), tw revisions.TagWatcher, revision string,
) *DeploymentController {
	gateways := kclient.New[*gateway.Gateway](client)
	gatewayClasses := kclient.New[*gateway.GatewayClass](client)
	dc := &DeploymentController{
		client:    client,
		clusterID: clusterID,
		clients:   map[schema.GroupVersionResource]getter{},
		env:       env,
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			_, err := c.Patch(context.Background(), name, types.ApplyPatchType, data, metav1.PatchOptions{
				Force:        &t,
				FieldManager: constants.ManagedGatewayController,
			}, subresources...)
			return err
		},
		gateways:       gateways,
		gatewayClasses: gatewayClasses,
		injectConfig:   webhookConfig,
		tagWatcher:     tw,
		revision:       revision,
	}
	dc.queue = controllers.NewQueue("gateway deployment",
		controllers.WithReconciler(dc.Reconcile),
		controllers.WithMaxAttempts(5))

	// Set up a handler that will add the parent Gateway object onto the queue.
	// The queue will only handle Gateway objects; if child resources (Service, etc) are updated we re-add
	// the Gateway to the queue and reconcile the state of the world.
	parentHandler := controllers.ObjectHandler(controllers.EnqueueForParentHandler(dc.queue, gvk.KubernetesGateway))

	dc.services = kclient.New[*corev1.Service](client)
	dc.services.AddEventHandler(parentHandler)
	dc.clients[gvr.Service] = NewUntypedWrapper(dc.services)

	dc.deployments = kclient.New[*appsv1.Deployment](client)
	dc.deployments.AddEventHandler(parentHandler)
	dc.clients[gvr.Deployment] = NewUntypedWrapper(dc.deployments)

	dc.serviceAccounts = kclient.New[*corev1.ServiceAccount](client)
	dc.serviceAccounts.AddEventHandler(parentHandler)
	dc.clients[gvr.ServiceAccount] = NewUntypedWrapper(dc.serviceAccounts)

	dc.namespaces = kclient.New[*corev1.Namespace](client)
	dc.namespaces.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// TODO: make this more intelligent, checking if something we care about has changed
		// requeue this namespace
		for _, gw := range dc.gateways.List(o.GetName(), klabels.Everything()) {
			dc.queue.AddObject(gw)
		}
	}))

	gateways.AddEventHandler(controllers.ObjectHandler(dc.queue.AddObject))
	gatewayClasses.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		for _, g := range dc.gateways.List(metav1.NamespaceAll, klabels.Everything()) {
			if string(g.Spec.GatewayClassName) == o.GetName() {
				dc.queue.AddObject(g)
			}
		}
	}))

	// On injection template change, requeue all gateways
	injectionHandler(func() {
		for _, gw := range dc.gateways.List(metav1.NamespaceAll, klabels.Everything()) {
			dc.queue.AddObject(gw)
		}
	})

	dc.tagWatcher.AddHandler(dc.HandleTagChange)

	return dc
}

func (d *DeploymentController) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync(
		"deployment controller",
		stop,
		d.namespaces.HasSynced,
		d.deployments.HasSynced,
		d.services.HasSynced,
		d.serviceAccounts.HasSynced,
		d.gateways.HasSynced,
		d.gatewayClasses.HasSynced,
		d.tagWatcher.HasSynced,
	)
	d.queue.Run(stop)
	controllers.ShutdownAll(d.namespaces, d.deployments, d.services, d.serviceAccounts, d.gateways, d.gatewayClasses)
}

// Reconcile takes in the name of a Gateway and ensures the cluster is in the desired state
func (d *DeploymentController) Reconcile(req types.NamespacedName) error {
	log := log.WithLabels("gateway", req)

	gw := d.gateways.Get(req.Name, req.Namespace)
	if gw == nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return nil
	}

	gc := d.gatewayClasses.Get(string(gw.Spec.GatewayClassName), "")
	if gc != nil {
		// We found the gateway class, but we do not implement it. Skip
		if !knownControllers.Contains(string(gc.Spec.ControllerName)) {
			return nil
		}
	} else {
		// Didn't find gateway class, and it wasn't an implicitly known one
		if _, f := classInfos[string(gw.Spec.GatewayClassName)]; !f {
			return nil
		}
	}

	// find the tag or revision indicated by the object
	selectedTag, ok := gw.Labels[label.IoIstioRev.Name]
	if !ok {
		ns := d.namespaces.Get(gw.Namespace, "")
		if ns == nil {
			return nil
		}
		selectedTag = ns.Labels[label.IoIstioRev.Name]
	}
	myTags := d.tagWatcher.GetMyTags()
	if !myTags.Contains(selectedTag) && !(selectedTag == "" && myTags.Contains("default")) {
		return nil
	}
	// TODO: Here we could check if the tag is set and matches no known tags, and handle that if we are default.

	// Matched class, reconcile it
	return d.configureIstioGateway(log, *gw)
}

func (d *DeploymentController) configureIstioGateway(log *istiolog.Scope, gw gateway.Gateway) error {
	// If user explicitly sets addresses, we are assuming they are pointing to an existing deployment.
	// We will not manage it in this case
	gi, f := classInfos[string(gw.Spec.GatewayClassName)]
	if !f {
		return nil
	}
	if !IsManaged(&gw.Spec) {
		log.Debug("skip disabled gateway")
		return nil
	}
	existingControllerVersion, overwriteControllerVersion, shouldHandle := ManagedGatewayControllerVersion(gw)
	if !shouldHandle {
		log.Debugf("skipping gateway which is managed by controller version %v", existingControllerVersion)
		return nil
	}
	log.Info("reconciling")

	defaultName := getDefaultName(gw.Name, &gw.Spec)
	input := TemplateInput{
		Gateway:        &gw,
		DeploymentName: model.GetOrDefault(gw.Annotations[gatewayNameOverride], defaultName),
		ServiceAccount: model.GetOrDefault(gw.Annotations[gatewaySAOverride], defaultName),
		Ports:          extractServicePorts(gw),
		ClusterID:      d.clusterID.String(),
		KubeVersion122: kube.IsAtLeastVersion(d.client, 22),
		Revision:       d.revision,
	}

	if overwriteControllerVersion {
		log.Debugf("write controller version, existing=%v", existingControllerVersion)
		if err := d.setGatewayControllerVersion(gw); err != nil {
			return fmt.Errorf("update gateway annotation: %v", err)
		}
	} else {
		log.Debugf("controller version existing=%v, no action needed", existingControllerVersion)
	}

	rendered, err := d.render(gi.templates, input)
	if err != nil {
		return fmt.Errorf("failed to render template: %v", err)
	}
	for _, t := range rendered {
		if err := d.apply(gi.controller, t); err != nil {
			return fmt.Errorf("apply failed: %v", err)
		}
	}

	log.Info("gateway updated")
	return nil
}

const (
	// ControllerVersionAnnotation is an annotation added to the Gateway by the controller specifying
	// the "controller version". The original intent of this was to work around
	// https://github.com/istio/istio/issues/44164, where we needed to transition from a global owner
	// to a per-revision owner. The newer version number allows forcing ownership, even if the other
	// version was otherwise expected to control the Gateway.
	// The version number has no meaning other than "larger numbers win".
	// Numbers are used to future-proof in case we need to do another migration in the future.
	ControllerVersionAnnotation = "gateway.istio.io/controller-version"
	// ControllerVersion is the current version of our controller logic. Known versions are:
	//
	// * 1.17 and older: version 1 OR no version at all, depending on patch release
	// * 1.18+: version 5
	//
	// 2, 3, and 4 were intentionally skipped to allow for the (unlikely) event we need to insert
	// another version between these
	ControllerVersion = 5
)

// ManagedGatewayControllerVersion determines the version of the controller managing this Gateway,
// and if we should manage this.
// See ControllerVersionAnnotation for motivations.
func ManagedGatewayControllerVersion(gw gateway.Gateway) (existing string, takeOver bool, manage bool) {
	cur, f := gw.Annotations[ControllerVersionAnnotation]
	if !f {
		// No current owner, we should take it over.
		return "", true, true
	}
	curNum, err := strconv.Atoi(cur)
	if err != nil {
		// We cannot parse it - must be some new schema we don't know about. We should assume we do not manage it.
		// In theory, this should never happen, unless we decide a number was a bad idea in the future.
		return cur, false, false
	}
	if curNum > ControllerVersion {
		// A newer version owns this gateway, let them handle it
		return cur, false, false
	}
	if curNum == ControllerVersion {
		// We already manage this at this version
		// We will manage it, but no need to attempt to apply the version annotation, which could race with newer versions
		return cur, false, true
	}
	// We are either newer or the same version of the last owner - we can take over. We need to actually
	// re-apply the annotation
	return cur, true, true
}

type derivedInput struct {
	TemplateInput

	// Inserted from injection config
	ProxyImage  string
	ProxyConfig *meshapi.ProxyConfig
	MeshConfig  *meshapi.MeshConfig
	Values      map[string]any
}

func (d *DeploymentController) render(templateName string, mi TemplateInput) ([]string, error) {
	cfg := d.injectConfig()

	template := cfg.Templates[templateName]
	if template == nil {
		return nil, fmt.Errorf("no %q template defined", templateName)
	}

	labelToMatch := map[string]string{"istio.io/gateway-name": mi.Name}
	proxyConfig := d.env.GetProxyConfigOrDefault(mi.Namespace, labelToMatch, nil, cfg.MeshConfig)
	input := derivedInput{
		TemplateInput: mi,
		ProxyImage: inject.ProxyImage(
			cfg.Values.Struct(),
			proxyConfig.GetImage(),
			mi.Annotations,
		),
		ProxyConfig: proxyConfig,
		MeshConfig:  cfg.MeshConfig,
		Values:      cfg.Values.Map(),
	}
	results, err := tmpl.Execute(template, input)
	if err != nil {
		return nil, err
	}

	return yml.SplitString(results), nil
}

func (d *DeploymentController) setGatewayControllerVersion(gws gateway.Gateway) error {
	patch := fmt.Sprintf(`{"apiVersion":"gateway.networking.k8s.io/v1beta1","kind":"Gateway","metadata":{"annotations":{"%s":"%d"}}}`,
		ControllerVersionAnnotation, ControllerVersion)

	log.Debugf("applying %v", patch)
	return d.patcher(gvr.KubernetesGateway, gws.GetName(), gws.GetNamespace(), []byte(patch))
}

// apply server-side applies a template to the cluster.
func (d *DeploymentController) apply(controller string, yml string) error {
	data := map[string]any{}
	err := yaml.Unmarshal([]byte(yml), &data)
	if err != nil {
		return err
	}
	us := unstructured.Unstructured{Object: data}
	// set managed-by label
	clabel := strings.ReplaceAll(controller, "/", "-")
	err = unstructured.SetNestedField(us.Object, clabel, "metadata", "labels", constants.ManagedGatewayLabel)
	if err != nil {
		return err
	}
	gvr, err := controllers.UnstructuredToGVR(us)
	if err != nil {
		return err
	}
	j, err := json.Marshal(us.Object)
	if err != nil {
		return err
	}
	canManage, resourceVersion := d.canManage(gvr, us.GetName(), us.GetNamespace())
	if !canManage {
		log.Debugf("skipping %v/%v/%v, already managed", gvr, us.GetName(), us.GetNamespace())
		return nil
	}
	// Ensure our canManage assertion is not stale
	us.SetResourceVersion(resourceVersion)

	log.Debugf("applying %v", string(j))
	if err := d.patcher(gvr, us.GetName(), us.GetNamespace(), j); err != nil {
		return fmt.Errorf("patch %v/%v/%v: %v", us.GroupVersionKind(), us.GetNamespace(), us.GetName(), err)
	}
	return nil
}

func (d *DeploymentController) HandleTagChange(newTags sets.Set[string]) {
	for _, gw := range d.gateways.List(metav1.NamespaceAll, klabels.Everything()) {
		d.queue.AddObject(gw)
	}
}

// canManage checks if a resource we are about to write should be managed by us. If the resource already exists
// but does not have the ManagedGatewayLabel, we won't overwrite it.
// This ensures we don't accidentally take over some resource we weren't supposed to, which could cause outages.
// Note K8s doesn't have a perfect way to "conditionally SSA", but its close enough (https://github.com/kubernetes/kubernetes/issues/116156).
func (d *DeploymentController) canManage(gvr schema.GroupVersionResource, name, namespace string) (bool, string) {
	store, f := d.clients[gvr]
	if !f {
		log.Warnf("unknown GVR %v", gvr)
		// Even though we don't know what it is, allow users to put the resource. We won't be able to
		// protect against overwrites though.
		return true, ""
	}
	obj := store.Get(name, namespace)
	if obj == nil {
		// no object, we can manage it
		return true, ""
	}
	_, managed := obj.GetLabels()[constants.ManagedGatewayLabel]
	// If object already exists, we can only manage it if it has the label
	return managed, obj.GetResourceVersion()
}

type TemplateInput struct {
	*gateway.Gateway
	DeploymentName string
	ServiceAccount string
	Ports          []corev1.ServicePort
	ClusterID      string
	KubeVersion122 bool
	Revision       string
}

func extractServicePorts(gw gateway.Gateway) []corev1.ServicePort {
	tcp := strings.ToLower(string(protocol.TCP))
	svcPorts := make([]corev1.ServicePort, 0, len(gw.Spec.Listeners)+1)
	svcPorts = append(svcPorts, corev1.ServicePort{
		Name:        "status-port",
		Port:        int32(15021),
		AppProtocol: &tcp,
	})
	portNums := sets.New[int32]()
	for i, l := range gw.Spec.Listeners {
		if portNums.Contains(int32(l.Port)) {
			continue
		}
		portNums.Insert(int32(l.Port))
		name := string(l.Name)
		if name == "" {
			// Should not happen since name is required, but in case an invalid resource gets in...
			name = fmt.Sprintf("%s-%d", strings.ToLower(string(l.Protocol)), i)
		}
		appProtocol := strings.ToLower(string(l.Protocol))
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:        name,
			Port:        int32(l.Port),
			AppProtocol: &appProtocol,
		})
	}
	return svcPorts
}

// UntypedWrapper wraps a typed reader to an untyped one, since Go cannot do it automatically.
type UntypedWrapper[T controllers.ComparableObject] struct {
	reader kclient.Reader[T]
}
type getter interface {
	Get(name, namespace string) controllers.Object
}

func NewUntypedWrapper[T controllers.ComparableObject](c kclient.Client[T]) getter {
	return UntypedWrapper[T]{c}
}

func (u UntypedWrapper[T]) Get(name, namespace string) controllers.Object {
	// DO NOT return u.reader.Get directly, or we run into issues with https://go.dev/tour/methods/12
	res := u.reader.Get(name, namespace)
	if controllers.IsNil(res) {
		return nil
	}
	return res
}

var _ getter = UntypedWrapper[*corev1.Service]{}
