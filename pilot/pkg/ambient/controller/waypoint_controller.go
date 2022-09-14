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

package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	listerappsv1 "k8s.io/client-go/listers/apps/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwlister "sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	istiogw "istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/gvk"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

type ProxySpecifier struct {
	Namespace      string `json:"namespace,omitempty"`
	ServiceAccount string `json:"serviceAccount,omitempty"`
	// TODO: "config set" determined by workload labels
}

func (i ProxySpecifier) MarshalText() (text []byte, err error) {
	return []byte(i.Namespace + "/" + i.ServiceAccount), nil
}

type Proxy struct {
	Name string `json:"name,omitempty"`
	IP   string `json:"ip,omitempty"`
}

type WaypointProxyController struct {
	client          kubelib.Client
	queue           controllers.Queue
	deployments     listerappsv1.DeploymentLister
	serviceAccounts listerv1.ServiceAccountLister
	gateways        gwlister.GatewayLister

	cluster cluster.ID

	injectConfig func() inject.WebhookConfig
}

const (
	istioMeshGatewayClass           = "istio-mesh"
	maxReconcileAttempts            = 5
	gatewayServiceAccountAnnotation = "istio.io/service-account"
)

var waypointLog = istiolog.RegisterScope("waypoint proxy", "", 0)

func init() {
	waypointLog.SetOutputLevel(istiolog.DebugLevel)
}

func NewWaypointProxyController(client kubelib.Client, clusterID cluster.ID, config func() inject.WebhookConfig) *WaypointProxyController {
	rc := &WaypointProxyController{
		client:       client,
		cluster:      clusterID,
		injectConfig: config,
	}

	rc.queue = controllers.NewQueue("waypoint proxy",
		controllers.WithReconciler(rc.Reconcile),
		controllers.WithMaxAttempts(maxReconcileAttempts))
	gateways := rc.client.GatewayAPIInformer().Gateway().V1alpha2().Gateways()
	rc.gateways = gateways.Lister()
	gateways.Informer().AddEventHandler(controllers.ObjectHandler(rc.queue.AddObject))

	deployments := rc.client.KubeInformer().Apps().V1().Deployments()
	rc.deployments = deployments.Lister()

	deployments.Informer().AddEventHandler(controllers.ObjectHandler(controllers.EnqueueForParentHandler(rc.queue, gvk.KubernetesGateway)))

	saInformer := rc.client.KubeInformer().Core().V1().ServiceAccounts()
	rc.serviceAccounts = saInformer.Lister()
	saInformer.Informer().AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// Anytime SA change, trigger all gateways in the namespace. This could probably be more efficient...
		gws, _ := gateways.Lister().Gateways(o.GetNamespace()).List(klabels.Everything())
		for _, gw := range gws {
			rc.queue.AddObject(gw)
		}
	}))

	return rc
}

func (rc *WaypointProxyController) Run(stop <-chan struct{}) {
	go rc.queue.Run(stop)
	<-stop
}

func (rc *WaypointProxyController) Reconcile(name types.NamespacedName) error {
	if rc.injectConfig().Values.Struct().GetGlobal().GetHub() == "" {
		// Mostly used to avoid issues with local runs
		return fmt.Errorf("injection config invalid, skipping reconile")
	}
	scopedLog := waypointLog.WithLabels("gateway", name.String())

	gw, err := rc.gateways.Gateways(name.Namespace).Get(name.Name)
	if err != nil || gw == nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		if err := controllers.IgnoreNotFound(err); err != nil {
			log.Errorf("unable to fetch Gateway: %v", err)
			return err
		}
		scopedLog.Debugf("gateway deleted")
		return rc.pruneGateway(name)
	}

	if gw.Spec.GatewayClassName != istioMeshGatewayClass {
		scopedLog.Debugf("mismatched class %q", gw.Spec.GatewayClassName)
		return rc.pruneGateway(name)
	}

	// by default, match all
	gatewaySA := gw.Annotations[gatewayServiceAccountAnnotation]
	existsProxies := rc.existsProxies(name)
	wantProxies := rc.wantProxies(name, gatewaySA)
	add, remove := wantProxies.Diff(existsProxies)
	if len(remove)+len(add) == 0 {
		scopedLog.Debugf("reconcile: remove %d, add %d. Have %d", len(remove), len(add), len(existsProxies))
		return nil
	}

	scopedLog.Infof("reconcile: remove %d, add %d. Have %d", len(remove), len(add), len(existsProxies))
	for _, sa := range remove {
		scopedLog.Infof("removing waypoint proxy %q", waypointName(sa))
		if err := rc.client.Kube().AppsV1().Deployments(gw.Namespace).Delete(context.Background(), waypointName(sa), metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("pod remove: %v", err)
		}

		msg := fmt.Sprintf("Removed waypoint proxy from %q namespace", gw.Namespace)
		if gatewaySA != "" {
			msg += fmt.Sprintf(" for %q service account", gatewaySA)
		}
		err := rc.UpdateStatus(gw, map[string]*istiogw.Condition{
			string(v1alpha2.GatewayConditionScheduled): {
				Reason:  string(v1alpha2.GatewayReasonScheduled),
				Message: msg,
			},
		})
		if err != nil {
			scopedLog.Errorf("unable to update Gateway status %v on delete: %v", gw.Name, err)
		}
	}
	for _, sa := range add {
		scopedLog.Infof("adding waypoint proxy %v", waypointName(sa))
		input := MergedInput{
			Namespace:      gw.Namespace,
			GatewayName:    gw.Name,
			UID:            string(gw.UID),
			ServiceAccount: sa,
			Cluster:        rc.cluster.String(),
		}
		proxyDeploy, err := rc.RenderDeploymentMerged(input)
		if err != nil {
			return err
		}

		if _, err := rc.client.Kube().AppsV1().Deployments(name.Namespace).Create(context.Background(), proxyDeploy, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("pod create: %v", err)
		}

		msg := fmt.Sprintf("Deployed waypoint proxy to %q namespace", gw.Namespace)
		if gatewaySA != "" {
			msg += fmt.Sprintf(" for %q service account", gatewaySA)
		}
		err = rc.UpdateStatus(gw, map[string]*istiogw.Condition{
			string(v1alpha2.GatewayConditionReady): {
				Reason:  string(v1alpha2.GatewayReasonReady),
				Message: msg,
			},
			string(v1alpha2.GatewayConditionScheduled): {
				Reason:  string(v1alpha2.GatewayReasonScheduled),
				Message: msg,
			},
		})
		if err != nil {
			scopedLog.Errorf("unable to update Gateway status %v on create: %v", gw.Name, err)
		}
	}
	return nil
}

func (rc *WaypointProxyController) existsProxies(name types.NamespacedName) sets.Set {
	haveProxies := sets.New()
	proxyLbl, _ := klabels.Parse("gateway.istio.io/managed=istio.io-mesh-controller,istio.io/gateway-name=" + name.Name)
	proxyDeployments, _ := rc.deployments.Deployments(name.Namespace).List(proxyLbl)
	for _, d := range proxyDeployments {
		p := d.Spec.Template
		haveProxies.Insert(p.Spec.ServiceAccountName)
	}
	return haveProxies
}

func (rc *WaypointProxyController) wantProxies(name types.NamespacedName, gatewaySA string) sets.Set {
	scopedLog := waypointLog.WithLabels("gateway", name.String())

	wantProxies := sets.New()
	serviceAccounts, _ := rc.serviceAccounts.ServiceAccounts(name.Namespace).List(klabels.Everything())
	for _, sa := range serviceAccounts {
		if gatewaySA != "" && sa.Name != gatewaySA {
			scopedLog.Debugf("skip service account %v, doesn't match gateway %v", sa.Name, gatewaySA)
			continue
		}
		wantProxies.Insert(sa.Name)
	}
	return wantProxies
}

func (rc *WaypointProxyController) UpdateStatus(gw *v1alpha2.Gateway, conditions map[string]*istiogw.Condition) error {
	if gw == nil {
		return nil
	}
	gws := &v1alpha2.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.KubernetesGateway.Kind,
			APIVersion: gvk.KubernetesGateway.Group + "/" + gvk.KubernetesGateway.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		},
		Status: v1alpha2.GatewayStatus{
			Conditions: istiogw.SetConditions(gw.Generation, nil, conditions),
		},
	}

	if _, err := rc.client.GatewayAPI().GatewayV1alpha2().Gateways(gw.Namespace).UpdateStatus(context.TODO(), gws, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update gateway status: %v", err)
	}

	log.Info("gateway %s/%s status updated", gw.Namespace, gw.Name)
	return nil
}

func (rc *WaypointProxyController) RenderDeploymentMerged(input MergedInput) (*appsv1.Deployment, error) {
	cfg := rc.injectConfig()

	// TODO watch for template changes, update the Deployment if it does
	podTemplate := cfg.Templates["waypoint"]
	if podTemplate == nil {
		return nil, fmt.Errorf("no waypoint template defined")
	}
	input.Image = inject.ProxyImage(cfg.Values.Struct(), cfg.MeshConfig.GetDefaultConfig().GetImage(), nil)
	input.ImagePullPolicy = cfg.Values.Struct().Global.GetImagePullPolicy()
	waypointBytes, err := tmpl.Execute(podTemplate, input)
	if err != nil {
		return nil, err
	}

	proxyPod, err := unmarshalDeploy([]byte(waypointBytes))
	if err != nil {
		return nil, fmt.Errorf("render: %v\n%v", err, waypointBytes)
	}
	return proxyPod, nil
}

// pruneGateway removes all proxies for the given Gateway
// This is not super required since Kubernetes GC can do it as well
func (rc *WaypointProxyController) pruneGateway(gw types.NamespacedName) error {
	proxyLbl, _ := klabels.Parse("gateway.istio.io/managed=istio.io-mesh-controller,istio.io/gateway-name=" + gw.Name)
	proxyDeployments, _ := rc.deployments.Deployments(gw.Namespace).List(proxyLbl)
	for _, d := range proxyDeployments {
		log.Infof("pruning waypoint proxy %s/%s", gw.Namespace, d.Name)
		if err := rc.client.Kube().AppsV1().Deployments(gw.Namespace).Delete(context.Background(), d.Name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("deployment remove: %v", err)
		}
	}
	return nil
}

func unmarshalDeploy(dyaml []byte) (*appsv1.Deployment, error) {
	deploy := &appsv1.Deployment{}
	if err := yaml.Unmarshal(dyaml, deploy); err != nil {
		return nil, err
	}

	return deploy, nil
}

// TODO: make waypoint suffix configurable
func waypointName(sa string) string {
	return sa + "-waypoint-proxy"
}

type MergedInput struct {
	GatewayName string

	Namespace       string
	UID             string
	ServiceAccount  string
	Cluster         string
	Image           string
	ImagePullPolicy string
}
