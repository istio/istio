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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	gateway "sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
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
// * Merge patch/Update cannot be used. If we always enforce that our object is *exactly* the same as
//   the in-cluster object we will get in endless loops due to other controllers that like to add annotations, etc.
//   If we chose to allow any unknown fields, then we would never be able to remove fields we added, as
//   we cannot tell if we created it or someone else did. SSA fixes these issues
// * SSA using client-go Apply libraries is almost a good choice, but most third-party clients (Istio, MCS, and gateway-api)
//   do not provide these libraries.
// * SSA using standard API types doesn't work well either: https://github.com/kubernetes-sigs/controller-runtime/issues/1669
// * This leaves YAML templates, converted to unstructured types and Applied with the dynamic client.
type DeploymentController struct {
	client    kube.Client
	queue     workqueue.RateLimitingInterface
	templates *template.Template
}

// NewDeploymentController constructs a DeploymentController and registers required informers.
// The controller will not start until Run() is called.
func NewDeploymentController(client kube.Client) *DeploymentController {
	q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	// Set up a handler that will add the parent Gateway object onto the queue.
	// The queue will only handle Gateway objects; if child resources (Service, etc) are updated we re-add
	// the Gateway to the queue and reconcile the state of the world.
	handler := controllers.LatestVersionHandlerFuncs(controllers.EnqueueForParentHandler(q, gvk.KubernetesGateway))

	// Use the full informer, since we are already fetching all Services for other purposes
	// If we somehow stop watching Services in the future we can add a label selector like below.
	client.KubeInformer().Core().V1().Services().Informer().
		AddEventHandler(handler)

	// For Deployments, this is the only controller watching. We can filter to just the deployments we care about
	client.KubeInformer().InformerFor(&appsv1.Deployment{}, func(k kubernetes.Interface, resync time.Duration) cache.SharedIndexInformer {
		return appsinformersv1.NewFilteredDeploymentInformer(
			k, metav1.NamespaceAll, resync, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			func(options *metav1.ListOptions) {
				options.LabelSelector = "gateway.istio.io/managed=istio.io-gateway-controller"
			},
		)
	}).AddEventHandler(handler)

	// Use the full informer; we are already watching all Gateways for the core Istiod logic
	client.GatewayAPIInformer().Gateway().V1alpha2().Gateways().Informer().
		AddEventHandler(controllers.LatestVersionHandlerFuncs(controllers.EnqueueForSelf(q)))

	return &DeploymentController{
		client:    client,
		queue:     q,
		templates: processTemplates(),
	}
}

func (d *DeploymentController) Run(stop <-chan struct{}) {
	defer d.queue.ShutDown()
	log.Infof("starting gateway deployment controller")
	go func() {
		// Process updates until we return false, which indicates the queue is terminated
		for d.processNextItem() {
		}
	}()
	<-stop
}

func (d *DeploymentController) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := d.queue.Get()
	if quit {
		return false
	}

	log.Debugf("handling update for %v", key)

	defer d.queue.Done(key)

	err := d.Reconcile(key.(types.NamespacedName))
	if err != nil {
		if d.queue.NumRequeues(key) < 5 {
			log.Errorf("error handling %v, retrying: %v", key, err)
			d.queue.AddRateLimited(key)
		} else {
			log.Errorf("error handling %v, and retry budget exceeded: %v", key, err)
		}
	}
	return true
}

// Reconcile takes in the name of a Gateway and ensures the cluster is in the desired state
func (d *DeploymentController) Reconcile(req types.NamespacedName) error {
	log := log.WithLabels("gateway", req)

	gw, err := d.client.GatewayAPIInformer().Gateway().V1alpha2().Gateways().Lister().Gateways(req.Namespace).Get(req.Name)
	if err != nil || gw == nil {
		log.Errorf("unable to fetch Gateway: %v", err)
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return controllers.IgnoreNotFound(err)
	}

	switch gw.Spec.GatewayClassName {
	case DefaultClassName:
		return d.configureIstioGateway(log, *gw)
	}

	return nil
}

func (d *DeploymentController) configureIstioGateway(log *istiolog.Scope, gw gateway.Gateway) error {
	// If user explicitly sets addresses, we are assuming they are pointing to an existing deployment.
	// We will not manage it in this case
	if !isManaged(&gw.Spec) {
		log.Debug("skip unmanaged gateway")
		return nil
	}
	log.Info("reconciling")

	if err := d.ApplyTemplate("service.yaml", serviceInput{gw, extractServicePorts(gw)}); err != nil {
		return fmt.Errorf("update service: %v", err)
	}
	log.Info("service updated")

	if err := d.ApplyTemplate("deployment.yaml", gw); err != nil {
		return fmt.Errorf("update deployment: %v", err)
	}
	log.Info("deployment updated")

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
			Conditions: setConditions(gw.Generation, nil, map[string]*condition{
				string(gateway.GatewayConditionScheduled): {
					reason:  "ResourcesAvailable",
					message: "Deployed gateway to the cluster",
				},
			}),
		},
	}
	if err := d.ApplyObject(gws, "status"); err != nil {
		return fmt.Errorf("update gateway status: %v", err)
	}
	log.Info("gateway updated")
	return nil
}

// ApplyTemplate renders a template with the given input and (server-side) applies the results to the cluster.
func (d *DeploymentController) ApplyTemplate(template string, input interface{}, subresources ...string) error {
	var buf bytes.Buffer
	if err := d.templates.ExecuteTemplate(&buf, template, input); err != nil {
		return err
	}
	data := map[string]interface{}{}
	err := yaml.Unmarshal(buf.Bytes(), &data)
	if err != nil {
		return err
	}
	us := unstructured.Unstructured{Object: data}
	gvr, err := controllers.UnstructuredToGVR(us)
	if err != nil {
		return err
	}
	j, err := json.Marshal(us.Object)
	if err != nil {
		return err
	}

	log.Debugf("applying %v", string(j))
	t := true
	c := d.client.Dynamic().Resource(gvr).Namespace(us.GetNamespace())
	_, err = c.Patch(context.Background(), us.GetName(), types.ApplyPatchType, j, metav1.PatchOptions{
		Force:        &t,
		FieldManager: ControllerName,
	}, subresources...)
	return err
}

// ApplyObject renders an object with the given input and (server-side) applies the results to the cluster.
func (d *DeploymentController) ApplyObject(obj controllers.Object, subresources ...string) error {
	j, err := config.ToJSON(obj)
	if err != nil {
		return err
	}
	obj.GetObjectKind()

	gvr, err := controllers.ObjectToGVR(obj)
	if err != nil {
		return err
	}
	log.Debugf("applying %v", string(j))
	t := true
	c := d.client.Dynamic().Resource(gvr).Namespace(obj.GetNamespace())
	_, err = c.Patch(context.Background(), obj.GetName(), types.ApplyPatchType, j, metav1.PatchOptions{
		Force:        &t,
		FieldManager: ControllerName,
	}, subresources...)
	return err
}

// Merge maps merges multiple maps. Latter maps take precedence over previous maps on overlapping fields
func mergeMaps(maps ...map[string]string) map[string]string {
	if len(maps) == 0 {
		return nil
	}
	res := make(map[string]string, len(maps[0]))
	for _, m := range maps {
		for k, v := range m {
			res[k] = v
		}
	}
	return res
}

type serviceInput struct {
	gateway.Gateway
	Ports []corev1.ServicePort
}

func extractServicePorts(gw gateway.Gateway) []corev1.ServicePort {
	svcPorts := make([]corev1.ServicePort, 0, len(gw.Spec.Listeners)+1)
	svcPorts = append(svcPorts, corev1.ServicePort{
		Name: "status-port",
		Port: int32(15021),
	})
	portNums := map[int32]struct{}{}
	for i, l := range gw.Spec.Listeners {
		if _, f := portNums[int32(l.Port)]; f {
			continue
		}
		portNums[int32(l.Port)] = struct{}{}
		name := fmt.Sprintf("%s-%d", strings.ToLower(string(l.Protocol)), i)
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name: name,
			Port: int32(l.Port),
		})
	}
	return svcPorts
}
