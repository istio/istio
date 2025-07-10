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
	"reflect"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	common_features "istio.io/istio/pkg/features"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
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
	client              kube.Client
	clusterID           cluster.ID
	env                 *model.Environment
	queue               controllers.Queue
	patcher             patcher
	gateways            kclient.Client[*gateway.Gateway]
	gatewayClasses      kclient.Client[*gateway.GatewayClass]
	listenerSets        kclient.Client[*gatewayx.XListenerSet]
	listenerSetByParent kclient.Index[types.NamespacedName, *gatewayx.XListenerSet]

	clients         map[schema.GroupVersionResource]getter
	injectConfig    func() inject.WebhookConfig
	deployments     kclient.Client[*appsv1.Deployment]
	services        kclient.Client[*corev1.Service]
	hpas            kclient.Client[*autoscalingv2.HorizontalPodAutoscaler]
	pdbs            kclient.Client[*policyv1.PodDisruptionBudget]
	configMaps      kclient.Client[*corev1.ConfigMap]
	serviceAccounts kclient.Client[*corev1.ServiceAccount]
	namespaces      kclient.Client[*corev1.Namespace]
	tagWatcher      revisions.TagWatcher
	revision        string
	systemNamespace string
}

// Patcher is a function that abstracts patching logic. This is largely because client-go fakes do not handle patching
type patcher func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error

// classInfo holds information about a gateway class
type classInfo struct {
	// controller name for this class
	controller string
	// controller label for this class
	controllerLabel string
	// description for this class
	description string
	// The key in the templates to use for this class
	templates string

	// defaultServiceType sets the default service type if one is not explicit set
	defaultServiceType corev1.ServiceType

	// disableRouteGeneration, if set, will make it so the controller ignores this class.
	disableRouteGeneration bool

	// supportsListenerSet declares whether a given class supports ListenerSet
	supportsListenerSet bool

	// disableNameSuffix, if set, will avoid appending -<class> to names
	disableNameSuffix bool

	// addressType is the default address type to report
	addressType gateway.AddressType
}

var classInfos = getClassInfos()

var builtinClasses = getBuiltinClasses()

func getBuiltinClasses() map[gateway.ObjectName]gateway.GatewayController {
	res := map[gateway.ObjectName]gateway.GatewayController{
		gateway.ObjectName(features.GatewayAPIDefaultGatewayClass): gateway.GatewayController(features.ManagedGatewayController),
	}

	if features.MultiNetworkGatewayAPI {
		res[constants.RemoteGatewayClassName] = constants.UnmanagedGatewayController
	}

	if features.EnableAmbientWaypoints {
		res[constants.WaypointGatewayClassName] = constants.ManagedGatewayMeshController
	}

	// N.B Ambient e/w gateways are just fancy waypoints, but we want a different
	// GatewayClass for better UX
	if features.EnableAmbientMultiNetwork {
		res[constants.EastWestGatewayClassName] = constants.ManagedGatewayEastWestController
	}
	return res
}

func getClassInfos() map[gateway.GatewayController]classInfo {
	m := map[gateway.GatewayController]classInfo{
		gateway.GatewayController(features.ManagedGatewayController): {
			controller:          features.ManagedGatewayController,
			description:         "The default Istio GatewayClass",
			templates:           "kube-gateway",
			defaultServiceType:  corev1.ServiceTypeLoadBalancer,
			addressType:         gateway.HostnameAddressType,
			controllerLabel:     constants.ManagedGatewayControllerLabel,
			supportsListenerSet: true,
		},
	}

	if features.MultiNetworkGatewayAPI {
		m[constants.UnmanagedGatewayController] = classInfo{
			// This represents a gateway that our control plane cannot discover directly via the API server.
			// We shouldn't generate Istio resources for it. We aren't programming this gateway.
			controller:             constants.UnmanagedGatewayController,
			description:            "Remote to this cluster. Does not deploy or affect configuration.",
			disableRouteGeneration: true,
			addressType:            gateway.HostnameAddressType,
			supportsListenerSet:    false,
		}
	}
	if features.EnableAmbientWaypoints {
		m[constants.ManagedGatewayMeshController] = classInfo{
			controller:          constants.ManagedGatewayMeshController,
			description:         "The default Istio waypoint GatewayClass",
			templates:           "waypoint",
			disableNameSuffix:   true,
			defaultServiceType:  corev1.ServiceTypeClusterIP,
			supportsListenerSet: false,
			// Report both. Consumers of the gateways can choose which they want.
			// In particular, Istio across different versions consumes different address types, so this retains compat
			addressType:     "",
			controllerLabel: constants.ManagedGatewayMeshControllerLabel,
		}
	}

	if features.EnableAmbientMultiNetwork {
		m[constants.ManagedGatewayEastWestController] = classInfo{
			controller:         constants.ManagedGatewayEastWestController,
			description:        "The default GatewayClass for Istio East West Gateways",
			templates:          "waypoint",
			disableNameSuffix:  true,
			defaultServiceType: corev1.ServiceTypeLoadBalancer,
			addressType:        "",
			controllerLabel:    constants.ManagedGatewayEastWestControllerLabel,
		}
	}
	return m
}

// NewDeploymentController constructs a DeploymentController and registers required informers.
// The controller will not start until Run() is called.
func NewDeploymentController(
	client kube.Client,
	clusterID cluster.ID,
	env *model.Environment,
	webhookConfig func() inject.WebhookConfig,
	injectionHandler func(fn func()),
	tw revisions.TagWatcher,
	revision string,
	systemNamespace string,
) *DeploymentController {
	filter := kclient.Filter{ObjectFilter: client.ObjectFilter()}
	gateways := kclient.NewFiltered[*gateway.Gateway](client, filter)
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
				FieldManager: features.ManagedGatewayController,
			}, subresources...)
			return err
		},
		gateways:        gateways,
		gatewayClasses:  gatewayClasses,
		injectConfig:    webhookConfig,
		tagWatcher:      tw,
		revision:        revision,
		systemNamespace: systemNamespace,
	}
	gatewaysByParamsRef := kclient.CreateIndex(gateways, "parametersRef", func(o *gateway.Gateway) []types.NamespacedName {
		p, err := fetchParameters(o)
		if p == nil || err != nil {
			return nil
		}
		return []types.NamespacedName{*p}
	})
	dc.queue = controllers.NewQueue("gateway deployment",
		controllers.WithReconciler(dc.Reconcile),
		controllers.WithMaxAttempts(5))

	if features.EnableAlphaGatewayAPI {
		dc.listenerSets = kclient.NewFiltered[*gatewayx.XListenerSet](client, filter)
		dc.listenerSetByParent = kclient.CreateIndex(dc.listenerSets, "parent", func(o *gatewayx.XListenerSet) []types.NamespacedName {
			return []types.NamespacedName{extractListenerSetParent(o)}
		})
		dc.listenerSets.AddEventHandler(controllers.TypedObjectHandler(func(o *gatewayx.XListenerSet) {
			dc.queue.Add(extractListenerSetParent(o))
		}))
	}

	// Set up a handler that will add the parent Gateway object onto the queue.
	// The queue will only handle Gateway objects; if child resources (Service, etc) are updated we re-add
	// the Gateway to the queue and reconcile the state of the world.
	parentHandler := controllers.ObjectHandler(controllers.EnqueueForParentHandler(dc.queue, gvk.KubernetesGateway))

	dc.services = kclient.NewFiltered[*corev1.Service](client, filter)
	dc.services.AddEventHandler(parentHandler)
	dc.clients[gvr.Service] = NewUntypedWrapper(dc.services)

	dc.configMaps = kclient.NewFiltered[*corev1.ConfigMap](client, filter)
	dc.configMaps.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// This could be a configmap referenced by a Gateway paramsRef
		impacted := gatewaysByParamsRef.Lookup(config.NamespacedName(o))
		for _, gw := range impacted {
			dc.queue.AddObject(gw)
		}
		// Or it could also be a global GatewayClass config
		classDefaults, classDefaultsF := o.GetLabels()[gatewayClassDefaults]
		if classDefaultsF && o.GetNamespace() == dc.systemNamespace {
			for _, gw := range dc.gateways.List(metav1.NamespaceAll, klabels.Everything()) {
				if string(gw.Spec.GatewayClassName) == classDefaults {
					dc.queue.AddObject(gw)
				}
			}
		}
	}))

	dc.deployments = kclient.NewFiltered[*appsv1.Deployment](client, filter)
	dc.deployments.AddEventHandler(parentHandler)
	dc.clients[gvr.Deployment] = NewUntypedWrapper(dc.deployments)

	dc.serviceAccounts = kclient.NewFiltered[*corev1.ServiceAccount](client, filter)
	dc.serviceAccounts.AddEventHandler(parentHandler)
	dc.clients[gvr.ServiceAccount] = NewUntypedWrapper(dc.serviceAccounts)

	dc.hpas = kclient.NewFiltered[*autoscalingv2.HorizontalPodAutoscaler](client, filter)
	dc.hpas.AddEventHandler(parentHandler)
	dc.clients[gvr.HorizontalPodAutoscaler] = NewUntypedWrapper(dc.hpas)

	dc.pdbs = kclient.NewFiltered[*policyv1.PodDisruptionBudget](client, filter)
	dc.pdbs.AddEventHandler(parentHandler)
	dc.clients[gvr.PodDisruptionBudget] = NewUntypedWrapper(dc.pdbs)

	dc.namespaces = kclient.NewFiltered[*corev1.Namespace](client, filter)
	dc.namespaces.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// TODO: make this more intelligent, checking if something we care about has changed
		// requeue this namespace
		for _, gw := range dc.gateways.List(o.GetName(), klabels.Everything()) {
			dc.queue.AddObject(gw)
		}
	}))

	// we check if the generation has changed on a gateway, or if the annotations or labels have been updated
	// reconciliation is expensive, so status updates for attachedRoutes can be skipped.

	gateways.AddEventHandler(
		controllers.FromEventHandler(func(o controllers.Event) {
			switch o.Event {
			case controllers.EventAdd:
				dc.queue.AddObject(o.New)
			case controllers.EventUpdate:
				if o.New.GetGeneration() != o.Old.GetGeneration() {
					dc.queue.AddObject(o.New)
					break
				}
				if !reflect.DeepEqual(o.New.GetLabels(), o.Old.GetLabels()) {
					dc.queue.AddObject(o.New)
					break
				}
				if !reflect.DeepEqual(o.New.GetAnnotations(), o.Old.GetAnnotations()) {
					dc.queue.AddObject(o.New)
					break
				}
				log.Debugf("skip unchanged gateway %s", o.New.GetName())
			case controllers.EventDelete:
				dc.queue.AddObject(o.Old)
			default:
				log.Errorf("unhandled event for gateway object %v", o)
			}
		}))

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

func extractListenerSetParent(o *gatewayx.XListenerSet) types.NamespacedName {
	n := o.Spec.ParentRef.Name
	ns := ptr.OrDefault(o.Spec.ParentRef.Namespace, gatewayx.Namespace(o.Namespace))
	return types.NamespacedName{
		Namespace: string(ns),
		Name:      string(n),
	}
}

func (d *DeploymentController) Run(stop <-chan struct{}) {
	kube.WaitForCacheSync(
		"deployment controller",
		stop,
		d.namespaces.HasSynced,
		d.deployments.HasSynced,
		d.services.HasSynced,
		d.configMaps.HasSynced,
		d.serviceAccounts.HasSynced,
		d.hpas.HasSynced,
		d.pdbs.HasSynced,
		d.gateways.HasSynced,
		d.gatewayClasses.HasSynced,
		d.tagWatcher.HasSynced,
	)
	d.queue.Run(stop)
	controllers.ShutdownAll(
		d.namespaces,
		d.deployments,
		d.services,
		d.configMaps,
		d.serviceAccounts,
		d.hpas,
		d.pdbs,
		d.gateways,
		d.gatewayClasses,
	)
}

// Reconcile takes in the name of a Gateway and ensures the cluster is in the desired state
func (d *DeploymentController) Reconcile(req types.NamespacedName) error {
	log := log.WithLabels("gateway", req)

	gw := d.gateways.Get(req.Name, req.Namespace)
	if gw == nil {
		log.Debugf("gateway no longer exists")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return nil
	}

	var controller gateway.GatewayController
	if gc := d.gatewayClasses.Get(string(gw.Spec.GatewayClassName), ""); gc != nil {
		controller = gc.Spec.ControllerName
	} else {
		if builtin, f := builtinClasses[gw.Spec.GatewayClassName]; f {
			controller = builtin
		}
	}
	ci, f := classInfos[controller]
	if !f {
		log.Debugf("skipping unknown controller %q", controller)
		return nil
	}
	log.Infof("reconciling gateway with controller %s", ci.controller)

	// find the tag or revision indicated by the object
	if !d.tagWatcher.IsMine(gw.ObjectMeta) {
		log.Debugf("gateway is not for this revision, skipping")
		return nil
	}
	// TODO: Here we could check if the tag is set and matches no known tags, and handle that if we are default.

	// Matched class, reconcile it
	return d.configureIstioGateway(log, *gw, ci)
}

func (d *DeploymentController) configureIstioGateway(log *istiolog.Scope, gw gateway.Gateway, gi classInfo) error {
	// If user explicitly sets addresses, we are assuming they are pointing to an existing deployment.
	// We will not manage it in this case
	if gi.templates == "" {
		log.Debug("skip gateway class without template")
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

	var ns *corev1.Namespace
	if d.namespaces != nil {
		ns = d.namespaces.Get(gw.Namespace, "")
	}
	proxyUID, proxyGID := inject.GetProxyIDs(ns)

	defaultName := getDefaultName(gw.Name, &gw.Spec, gi.disableNameSuffix)

	serviceType := gi.defaultServiceType
	if o, f := gw.Annotations[annotation.NetworkingServiceType.Name]; f {
		serviceType = corev1.ServiceType(o)
	}
	listenersFromListenerSets := []gateway.Listener{}
	if d.listenerSets != nil {
		sets := d.listenerSetByParent.Lookup(config.NamespacedName(&gw))
		for _, set := range sets {
			if !namespaceAcceptedByAllowListeners(set.Namespace, &gw, func(s string) *corev1.Namespace {
				return d.namespaces.Get(s, "")
			}) {
				continue
			}
			for _, listener := range set.Spec.Listeners {
				s := convertListenerSetToListener(listener)
				listenersFromListenerSets = append(listenersFromListenerSets, s)
			}
		}
	}

	input := TemplateInput{
		Gateway:        &gw,
		DeploymentName: model.GetOrDefault(gw.Annotations[annotation.GatewayNameOverride.Name], defaultName),
		ServiceAccount: model.GetOrDefault(gw.Annotations[annotation.GatewayServiceAccount.Name], defaultName),
		Ports:          extractServicePorts(gw, listenersFromListenerSets),
		ClusterID:      d.clusterID.String(),

		KubeVersion:               kube.GetVersionAsInt(d.client),
		Revision:                  d.revision,
		ServiceType:               serviceType,
		ProxyUID:                  proxyUID,
		ProxyGID:                  proxyGID,
		CompliancePolicy:          common_features.CompliancePolicy,
		InfrastructureLabels:      gw.GetLabels(),
		InfrastructureAnnotations: gw.GetAnnotations(),
		GatewayNameLabel:          label.IoK8sNetworkingGatewayGatewayName.Name,
		IsEastWestGateway:         features.EnableAmbientMultiNetwork && gi.controller == constants.ManagedGatewayEastWestController,
		ControllerLabel:           gi.controllerLabel,
	}

	// Default to the gateway labels/annotations and overwrite if infrastructure labels/annotations are set
	input.InfrastructureLabels = extractInfrastructureLabels(gw)
	input.InfrastructureAnnotations = extractInfrastructureAnnotations(gw)
	d.setLabelOverrides(gw, input)

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
		// Just log error, we do not need to retry since rendering errors are not ephemeral errors
		log.Errorf("error rendering templates: %v", err)
		return nil
	}
	for _, t := range rendered {
		if err := d.apply(gi.controller, t); err != nil {
			return fmt.Errorf("apply failed: %v", err)
		}
	}

	log.Info("gateway updated")
	return nil
}

func (d *DeploymentController) setLabelOverrides(gw gateway.Gateway, input TemplateInput) {
	isWaypointGateway := strings.Contains(string(gw.Spec.GatewayClassName), "waypoint")
	isEastWestGateway := strings.Contains(string(gw.Spec.GatewayClassName), "east-west")

	var hasAmbientLabel bool
	if _, ok := gw.Labels[label.IoIstioDataplaneMode.Name]; ok {
		hasAmbientLabel = true
	}
	if _, ok := input.InfrastructureLabels[label.IoIstioDataplaneMode.Name]; ok {
		hasAmbientLabel = true
	}
	// If no ambient redirection label is set explicitly, explicitly disable.
	// TODO this sprays ambient annotations/labels all over EVER gateway resource (serviceaccts, services, etc)
	if features.EnableAmbientWaypoints && !isWaypointGateway && !isEastWestGateway && !hasAmbientLabel {
		input.InfrastructureLabels[label.IoIstioDataplaneMode.Name] = constants.DataplaneModeNone
	}

	// Default the network label for waypoints if not explicitly set in gateway's labels
	network := d.injectConfig().Values.Struct().GetGlobal().GetNetwork()
	if _, ok := gw.GetLabels()[label.TopologyNetwork.Name]; !ok && network != "" && (isWaypointGateway || isEastWestGateway) {
		input.InfrastructureLabels[label.TopologyNetwork.Name] = d.injectConfig().Values.Struct().GetGlobal().GetNetwork()
	}
}

func extractInfrastructureLabels(gw gateway.Gateway) map[string]string {
	return extractInfrastructureMetadata(gw.Spec.Infrastructure, true, gw)
}

func extractInfrastructureAnnotations(gw gateway.Gateway) map[string]string {
	return extractInfrastructureMetadata(gw.Spec.Infrastructure, false, gw)
}

func translateInfraMeta[K ~string, V ~string](meta map[K]V) map[string]string {
	infra := make(map[string]string, len(meta))
	for k, v := range meta {
		if strings.HasPrefix(string(k), "gateway.networking.k8s.io/") {
			continue // ignore this prefix to avoid conflicts
		}
		infra[string(k)] = string(v)
	}
	return infra
}

func extractInfrastructureMetadata(gwInfra *gatewayv1.GatewayInfrastructure, isLabel bool, gw gateway.Gateway) map[string]string {
	if gwInfra != nil && isLabel && gwInfra.Labels != nil {
		return translateInfraMeta(gwInfra.Labels)
	}
	if gwInfra != nil && !isLabel && gwInfra.Annotations != nil {
		return translateInfraMeta(gwInfra.Annotations)
	}
	if !features.EnableGatewayAPICopyLabelsAnnotations {
		return make(map[string]string)
	}
	if isLabel {
		if gw.GetLabels() == nil {
			return make(map[string]string)
		}
		return maps.Clone(gw.GetLabels())
	}
	if gw.GetAnnotations() == nil {
		return make(map[string]string)
	}
	return maps.Clone(gw.GetAnnotations())
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

	var templateOverlays []map[string]string

	classConfigs := d.configMaps.List(d.systemNamespace, klabels.SelectorFromValidatedSet(map[string]string{
		gatewayClassDefaults: string(mi.Spec.GatewayClassName),
	}))
	if len(classConfigs) > 0 {
		classConfig := controllers.OldestObject(classConfigs)
		templateOverlays = append(templateOverlays, classConfig.Data)
	}
	params, err := fetchParameters(mi.Gateway)
	if err != nil {
		return nil, fmt.Errorf("invalid parameters: %v", err)
	}
	if params != nil {
		cm := d.configMaps.Get(params.Name, params.Namespace)
		if cm == nil {
			return nil, fmt.Errorf("parametersRef targeting configmap %q, but configmap does not exist", params)
		}
		templateOverlays = append(templateOverlays, cm.Data)
	}

	labelToMatch := map[string]string{label.IoK8sNetworkingGatewayGatewayName.Name: mi.Name}
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

	rawOutput := yml.SplitString(results)
	transformedOutput := make([]string, 0, len(rawOutput))
	for _, output := range rawOutput {
		to, err := applyOverlay(output, templateOverlays)
		if err != nil {
			return nil, err
		}
		if to != "" {
			transformedOutput = append(transformedOutput, to)
		}
	}
	return transformedOutput, nil
}

var supportedOverlays = sets.New(
	"deployment",
	"service",
	"serviceAccount",
	"horizontalPodAutoscaler",
	"podDisruptionBudget",
)

var requiredOverlays = sets.New(
	"horizontalPodAutoscaler",
	"podDisruptionBudget",
)

func applyOverlay(object string, overlaysList []map[string]string) (string, error) {
	var ik crd.IstioKind
	if err := yaml.Unmarshal([]byte(object), &ik); err != nil {
		return "", fmt.Errorf("failed to find kind: %v", err)
	}
	gv, err := schema.ParseGroupVersion(ik.TypeMeta.APIVersion)
	if err != nil {
		return "", fmt.Errorf("failed to find kind: %v", err)
	}
	kind := &schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: ik.TypeMeta.Kind}

	var data any
	var key string
	switch kind.Kind {
	case gvk.Deployment.Kind:
		data = &appsv1.Deployment{}
		key = "deployment"
	case gvk.Service.Kind:
		data = &corev1.Service{}
		key = "service"
	case gvk.ServiceAccount.Kind:
		data = &corev1.ServiceAccount{}
		key = "serviceAccount"
	case gvk.HorizontalPodAutoscaler.Kind:
		data = &autoscalingv2.HorizontalPodAutoscaler{}
		key = "horizontalPodAutoscaler"
	case gvk.PodDisruptionBudget.Kind:
		data = &policyv1.PodDisruptionBudget{}
		key = "podDisruptionBudget"
	default:
		return "", fmt.Errorf("unknown overlay kind %q", kind.Kind)
	}
	applied := false
	for _, overlays := range overlaysList {
		for k := range overlays {
			if !supportedOverlays.Contains(k) {
				return "", fmt.Errorf("unsupported overlay %q (supported: %v)", k, sets.SortedList(supportedOverlays))
			}
		}
		overlay, f := overlays[key]
		if !f {
			continue
		}
		b, err := strategicMergePatchYAML([]byte(object), []byte(overlay), data)
		if err != nil {
			return "", fmt.Errorf("strategic merge patch failed: %v", err)
		}
		applied = true
		object = string(b)
	}
	if !applied && requiredOverlays.Contains(key) {
		return "", nil
	}

	var finalIK crd.IstioKind
	if err := yaml.Unmarshal([]byte(object), &finalIK); err != nil {
		return "", fmt.Errorf("failed to find final kind: %v", err)
	}

	a, b := ik.ObjectMeta, finalIK.ObjectMeta
	if !(a.Name == b.Name &&
		a.GenerateName == b.GenerateName &&
		a.Namespace == b.Namespace &&
		a.UID == b.UID &&
		a.ResourceVersion == b.ResourceVersion &&
		a.Generation == b.Generation &&
		a.CreationTimestamp == b.CreationTimestamp &&
		a.DeletionTimestamp == b.DeletionTimestamp &&
		a.DeletionGracePeriodSeconds == b.DeletionGracePeriodSeconds &&
		reflect.DeepEqual(a.OwnerReferences, b.OwnerReferences) &&
		slices.Equal(a.Finalizers, b.Finalizers)) {
		return "", fmt.Errorf("illegal metadata change")
	}
	// We could deep equal here but its a bit more tedious, so just never allow setting it
	if len(a.ManagedFields) != 0 || len(b.ManagedFields) != 0 {
		return "", fmt.Errorf("illegal metadata change")
	}

	return object, nil
}

// fetchParameters returns the infrastructure parameters for the Gateway. This is currently always a local configmap so we return the name only.
// An error is returned if the parameter is invalid. This does not check the configmap exists, though.
// If no parameter is specified, no name or error is returned.
func fetchParameters(gw *gateway.Gateway) (*types.NamespacedName, error) {
	if gw.Spec.Infrastructure != nil && gw.Spec.Infrastructure.ParametersRef != nil {
		pr := gw.Spec.Infrastructure.ParametersRef
		if string(pr.Kind) == gvk.ConfigMap.Kind && string(pr.Group) == gvk.ConfigMap.Group {
			return &types.NamespacedName{
				Namespace: gw.Namespace,
				Name:      pr.Name,
			}, nil
		}
		return nil, fmt.Errorf("unknown infrastructure parameters type %v/%v", pr.Group, pr.Kind)
	}
	return nil, nil
}

func (d *DeploymentController) setGatewayControllerVersion(gws gateway.Gateway) error {
	patch := fmt.Sprintf(`{"apiVersion":"gateway.networking.k8s.io/v1beta1","kind":"Gateway","metadata":{"annotations":{"%s":"%d"}}}`,
		ControllerVersionAnnotation, ControllerVersion)

	log.Debugf("applying %v", patch)
	// Use status RBAC so we do not require full Gateway write.
	// `status` write can modify annotations, and we already need to write status anyway so we have the permission.
	return d.patcher(gvr.KubernetesGateway, gws.GetName(), gws.GetNamespace(), []byte(patch), "status")
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
	err = unstructured.SetNestedField(us.Object, clabel, "metadata", "labels", label.GatewayManaged.Name)
	if err != nil {
		return err
	}
	gvr, err := controllers.UnstructuredToGVR(us)
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

	// Because in 1.24 we removed old label "istio.io/gateway-name", in order to not mutate the deployment.spec.Selector during upgrade.
	// we always use the old `selector` value
	if gvr.Resource == "deployments" {
		deployment := d.deployments.Get(us.GetName(), us.GetNamespace())
		if deployment != nil && deployment.Spec.Selector.MatchLabels["istio.io/gateway-name"] != "" {
			us.Object["spec"].(map[string]any)["selector"] = deployment.Spec.Selector
			// nolint lll
			us.Object["spec"].(map[string]any)["template"].(map[string]any)["metadata"].(map[string]any)["labels"].(map[string]any)["istio.io/gateway-name"] = deployment.Spec.Template.ObjectMeta.Labels["istio.io/gateway-name"]
		}
	}

	j, err := json.Marshal(us.Object)
	if err != nil {
		return err
	}
	log.Debugf("applying %v", string(j))
	if err := d.patcher(gvr, us.GetName(), us.GetNamespace(), j); err != nil {
		return fmt.Errorf("patch %v/%v/%v: %v", us.GroupVersionKind(), us.GetNamespace(), us.GetName(), err)
	}
	return nil
}

func (d *DeploymentController) HandleTagChange(newTags sets.String) {
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
	_, managed := obj.GetLabels()[label.GatewayManaged.Name]
	// If object already exists, we can only manage it if it has the label
	return managed, obj.GetResourceVersion()
}

type TemplateInput struct {
	*gateway.Gateway
	DeploymentName            string
	ServiceAccount            string
	Ports                     []corev1.ServicePort
	ServiceType               corev1.ServiceType
	ClusterID                 string
	KubeVersion               int
	Revision                  string
	ProxyUID                  int64
	ProxyGID                  int64
	CompliancePolicy          string
	InfrastructureLabels      map[string]string
	InfrastructureAnnotations map[string]string
	GatewayNameLabel          string
	IsEastWestGateway         bool
	ControllerLabel           string
}

func extractServicePorts(gw gateway.Gateway, listenerSets []gateway.Listener) []corev1.ServicePort {
	tcp := strings.ToLower(string(protocol.TCP))
	svcPorts := make([]corev1.ServicePort, 0, len(listenerSets)+len(gw.Spec.Listeners)+1)
	svcPorts = append(svcPorts, corev1.ServicePort{
		Name:        "status-port",
		Port:        int32(15021),
		AppProtocol: &tcp,
	})
	portNums := sets.New[int32]()
	allListeners := append(slices.Clone(gw.Spec.Listeners), listenerSets...)
	for i, l := range allListeners {
		if portNums.Contains(int32(l.Port)) {
			continue
		}
		portNums.Insert(int32(l.Port))
		name := sanitizeListenerNameForPort(string(l.Name))
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

// ListenerName allows periods and 253 chars.
// We map this to service port name which does not allow period and only 63 chars.
func sanitizeListenerNameForPort(s string) string {
	// In theory, this mapping can result in a duplicate, but probably not likely
	s = strings.ReplaceAll(s, ".", "-")
	if len(s) <= 63 {
		return s
	}
	return s[:63]
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

// strategicMergePatchYAML is a small fork of strategicpatch.StrategicMergePatch to allow YAML patches
// This avoids expensive conversion from YAML to JSON
func strategicMergePatchYAML(originalYAML []byte, patchYAML []byte, dataStruct any) ([]byte, error) {
	schema, err := strategicpatch.NewPatchMetaFromStruct(dataStruct)
	if err != nil {
		return nil, err
	}

	originalMap, err := patchHandleUnmarshal(originalYAML)
	if err != nil {
		return nil, err
	}
	patchMap, err := patchHandleUnmarshal(patchYAML)
	if err != nil {
		return nil, err
	}

	result, err := strategicpatch.StrategicMergeMapPatchUsingLookupPatchMeta(originalMap, patchMap, schema)
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

func patchHandleUnmarshal(j []byte) (map[string]any, error) {
	if j == nil {
		j = []byte("{}")
	}

	m := map[string]any{}
	err := yaml.Unmarshal(j, &m)
	if err != nil {
		return nil, mergepatch.ErrBadJSONDoc
	}
	return m, nil
}
