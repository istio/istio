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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	gateway "sigs.k8s.io/gateway-api/apis/v1beta1"
	lister "sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1beta1"
	"sigs.k8s.io/yaml"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
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
	client             kube.Client
	queue              controllers.Queue
	patcher            patcher
	gatewayLister      lister.GatewayLister
	gatewayClassLister lister.GatewayClassLister

	serviceInformer    cache.SharedIndexInformer
	serviceHandle      cache.ResourceEventHandlerRegistration
	deploymentInformer cache.SharedIndexInformer
	deploymentHandle   cache.ResourceEventHandlerRegistration
	gwInformer         cache.SharedIndexInformer
	gwHandle           cache.ResourceEventHandlerRegistration
	gwClassInformer    cache.SharedIndexInformer
	gwClassHandle      cache.ResourceEventHandlerRegistration
	injectConfig       func() inject.WebhookConfig
}

// Patcher is a function that abstracts patching logic. This is largely because client-go fakes do not handle patching
type patcher func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error

// classInfo holds information about a gateway class
type classInfo struct {
	// controller name for this class
	controller string
	// The key in the templates to use for this class
	templates string
	// enabled determines if we should handle a gateway
	enabled func(gw *gateway.Gateway) bool
	// conditions to set on gateway status
	conditions func(gw *gateway.Gateway) map[string]*condition
	// reportGatewayClassStatus, if enabled, will set the GatewayClass to be accepted when it is first created.
	// nolint: unused
	reportGatewayClassStatus bool
}

var classInfos = map[string]classInfo{
	DefaultClassName: {
		controller: ControllerName,
		templates:  "kube-gateway",
		enabled: func(gw *gateway.Gateway) bool {
			// Some gateways are manually managed, ignore them
			return IsManaged(&gw.Spec)
		},
		conditions: func(gw *gateway.Gateway) map[string]*condition {
			return map[string]*condition{
				// Just mark it as accepted, rest are set by the controller reading Gateway
				string(gateway.GatewayConditionAccepted): {
					reason:  string(gateway.GatewayReasonAccepted),
					message: "Deployed gateway to the cluster",
				},
				// nolint: staticcheck // Deprecated condition, set both until 1.17
				string(gateway.GatewayConditionScheduled): {
					reason:  "ResourcesAvailable",
					message: "Deployed gateway to the cluster",
				},
			}
		},
	},
	constants.WaypointGatewayClassName: {
		controller: constants.ManagedGatewayMeshController,
		templates:  "waypoint",
		enabled: func(gw *gateway.Gateway) bool {
			// we manage all "mesh" gateways
			return true
		},
		reportGatewayClassStatus: true,
		conditions: func(gw *gateway.Gateway) map[string]*condition {
			msg := fmt.Sprintf("Deployed waypoint proxy to %q namespace", gw.Namespace)
			forSa := gw.Annotations[constants.WaypointServiceAccount]
			if forSa != "" {
				msg += fmt.Sprintf(" for %q service account", forSa)
			}
			accept := msg
			if unexpectedWaypointListener(gw) {
				accept += "; WARN: expected a single listener on port 15008 with protocol \"ALL\""
			}
			// For waypoint, we don't have another controller setting status, so set all fields
			return map[string]*condition{
				string(gateway.GatewayConditionReady): {
					reason:  string(gateway.GatewayReasonReady),
					message: msg,
				},
				string(gateway.ListenerConditionProgrammed): {
					reason:  string(gateway.GatewayReasonProgrammed),
					message: msg,
				},
				string(gateway.GatewayConditionAccepted): {
					reason:  string(gateway.GatewayReasonAccepted),
					message: accept,
				},
			}
		},
	},
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
func NewDeploymentController(client kube.Client, webhookConfig func() inject.WebhookConfig, injectionHandler func(fn func())) *DeploymentController {
	gw := client.GatewayAPIInformer().Gateway().V1beta1().Gateways()
	gwc := client.GatewayAPIInformer().Gateway().V1beta1().GatewayClasses()
	dc := &DeploymentController{
		client: client,
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			_, err := c.Patch(context.Background(), name, types.ApplyPatchType, data, metav1.PatchOptions{
				Force:        &t,
				FieldManager: ControllerName,
			}, subresources...)
			return err
		},
		gatewayLister:      gw.Lister(),
		gatewayClassLister: gwc.Lister(),
		injectConfig:       webhookConfig,
	}
	dc.queue = controllers.NewQueue("gateway deployment",
		controllers.WithReconciler(dc.Reconcile),
		controllers.WithMaxAttempts(5))

	// Set up a handler that will add the parent Gateway object onto the queue.
	// The queue will only handle Gateway objects; if child resources (Service, etc) are updated we re-add
	// the Gateway to the queue and reconcile the state of the world.
	handler := controllers.ObjectHandler(controllers.EnqueueForParentHandler(dc.queue, gvk.KubernetesGateway))

	// Use the full informer, since we are already fetching all Services for other purposes
	// If we somehow stop watching Services in the future we can add a label selector like below.
	dc.serviceInformer = client.KubeInformer().Core().V1().Services().Informer()
	dc.serviceHandle, _ = client.KubeInformer().Core().V1().Services().Informer().
		AddEventHandler(handler)

	// For Deployments, this is the only controller watching. We can filter to just the deployments we care about
	deployInformer := client.KubeInformer().InformerFor(&appsv1.Deployment{}, func(k kubernetes.Interface, resync time.Duration) cache.SharedIndexInformer {
		return appsinformersv1.NewFilteredDeploymentInformer(
			k, metav1.NamespaceAll, resync, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			func(options *metav1.ListOptions) {
				// All types of gateways have this label
				options.LabelSelector = constants.ManagedGatewayLabel
			},
		)
	})
	_ = deployInformer.SetTransform(kube.StripUnusedFields)
	dc.deploymentHandle, _ = deployInformer.AddEventHandler(handler)
	dc.deploymentInformer = deployInformer

	// Use the full informer; we are already watching all Gateways for the core Istiod logic
	dc.gwInformer = gw.Informer()
	dc.gwHandle, _ = dc.gwInformer.AddEventHandler(controllers.ObjectHandler(dc.queue.AddObject))
	dc.gwClassInformer = gwc.Informer()
	dc.gwClassHandle, _ = dc.gwClassInformer.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		gws, _ := dc.gatewayLister.List(klabels.Everything())
		for _, g := range gws {
			if string(g.Spec.GatewayClassName) == o.GetName() {
				dc.queue.AddObject(g)
			}
		}
	}))

	// On injection template change, requeue all gateways
	injectionHandler(func() {
		gws, _ := dc.gatewayLister.List(klabels.Everything())
		for _, gw := range gws {
			dc.queue.AddObject(gw)
		}
	})

	return dc
}

func (d *DeploymentController) Run(stop <-chan struct{}) {
	d.queue.Run(stop)
	_ = d.serviceInformer.RemoveEventHandler(d.serviceHandle)
	_ = d.deploymentInformer.RemoveEventHandler(d.deploymentHandle)
	_ = d.gwInformer.RemoveEventHandler(d.gwHandle)
	_ = d.gwClassInformer.RemoveEventHandler(d.gwClassHandle)
}

// Reconcile takes in the name of a Gateway and ensures the cluster is in the desired state
func (d *DeploymentController) Reconcile(req types.NamespacedName) error {
	log := log.WithLabels("gateway", req)

	gw, err := d.gatewayLister.Gateways(req.Namespace).Get(req.Name)
	if err != nil || gw == nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		if err := controllers.IgnoreNotFound(err); err != nil {
			log.Errorf("unable to fetch Gateway: %v", err)
			return err
		}
		return nil
	}

	gc, _ := d.gatewayClassLister.Get(string(gw.Spec.GatewayClassName))
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
	if !gi.enabled(&gw) {
		log.Debug("skip disabled gateway")
		return nil
	}
	log.Info("reconciling")

	defaultName := getDefaultName(gw.Name, &gw.Spec)
	deploymentName := defaultName
	if nameOverride, exists := gw.Annotations[gatewayNameOverride]; exists {
		deploymentName = nameOverride
	}

	gatewaySA := defaultName
	if saOverride, exists := gw.Annotations[gatewaySAOverride]; exists {
		gatewaySA = saOverride
	}

	input := TemplateInput{
		Gateway:        &gw,
		DeploymentName: deploymentName,
		ServiceAccount: gatewaySA,
		Ports:          extractServicePorts(gw),
		KubeVersion122: kube.IsAtLeastVersion(d.client, 22),
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

	cond := gi.conditions(&gw)
	gws := &gateway.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.KubernetesGateway.Kind,
			APIVersion: gvk.KubernetesGateway.Group + "/" + gvk.KubernetesGateway.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		},
		Status: gateway.GatewayStatus{
			Conditions: setConditions(gw.Generation, nil, cond),
		},
	}
	if err := d.ApplyObject(gws, "status"); err != nil {
		return fmt.Errorf("update gateway status: %v", err)
	}
	log.Info("gateway updated")
	return nil
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
	input := derivedInput{
		TemplateInput: mi,
		ProxyImage: inject.ProxyImage(
			cfg.Values.Struct(),
			cfg.MeshConfig.GetDefaultConfig().GetImage(),
			mi.Annotations,
		),
		ProxyConfig: cfg.MeshConfig.GetDefaultConfig(),
		MeshConfig:  cfg.MeshConfig,
		Values:      cfg.Values.Map(),
	}
	results, err := tmpl.Execute(template, input)
	if err != nil {
		return nil, err
	}

	return yml.SplitString(results), nil
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

	log.Debugf("applying %v", string(j))
	if err := d.patcher(gvr, us.GetName(), us.GetNamespace(), j); err != nil {
		return fmt.Errorf("patch %v/%v/%v: %v", us.GroupVersionKind(), us.GetNamespace(), us.GetName(), err)
	}
	return nil
}

// ApplyObject renders an object with the given input and (server-side) applies the results to the cluster.
func (d *DeploymentController) ApplyObject(obj controllers.Object, subresources ...string) error {
	j, err := config.ToJSON(obj)
	if err != nil {
		return err
	}

	gvr, err := controllers.ObjectToGVR(obj)
	if err != nil {
		return err
	}
	log.Debugf("applying %v", string(j))

	return d.patcher(gvr, obj.GetName(), obj.GetNamespace(), j, subresources...)
}

type TemplateInput struct {
	*gateway.Gateway
	DeploymentName string
	ServiceAccount string
	Ports          []corev1.ServicePort
	KubeVersion122 bool
}

func extractServicePorts(gw gateway.Gateway) []corev1.ServicePort {
	tcp := strings.ToLower(string(protocol.TCP))
	svcPorts := make([]corev1.ServicePort, 0, len(gw.Spec.Listeners)+1)
	svcPorts = append(svcPorts, corev1.ServicePort{
		Name:        "status-port",
		Port:        int32(15021),
		AppProtocol: &tcp,
	})
	portNums := map[int32]struct{}{}
	for i, l := range gw.Spec.Listeners {
		if _, f := portNums[int32(l.Port)]; f {
			continue
		}
		portNums[int32(l.Port)] = struct{}{}
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

// Gateway currently requires a listener (https://github.com/kubernetes-sigs/gateway-api/pull/1596).
// We don't *really* care about the listener, but it may make sense to add a warning if users do not
// configure it in an expected way so that we have consistency and can make changes in the future as needed.
// We could completely reject but that seems more likely to cause pain.
func unexpectedWaypointListener(gw *gateway.Gateway) bool {
	if len(gw.Spec.Listeners) != 1 {
		return true
	}
	l := gw.Spec.Listeners[0]
	if l.Port != 15008 {
		return true
	}
	if l.Protocol != "ALL" {
		return true
	}
	return false
}
