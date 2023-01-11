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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwlister "sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	meshapi "istio.io/api/mesh/v1alpha1"
	istiogw "istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
	"k8s.io/client-go/tools/cache"
)

type WaypointProxyController struct {
	client          kubelib.Client
	queue           controllers.Queue
	serviceAccounts listerv1.ServiceAccountLister
	saInformer      cache.SharedIndexInformer
	gatewayInformer cache.SharedIndexInformer
	gateways        gwlister.GatewayLister
	patcher         istiogw.Patcher

	cluster cluster.ID

	injectConfig func() inject.WebhookConfig
}

var waypointLog = istiolog.RegisterScope("waypointproxy", "", 0)
var waypointFM = "waypoint proxy controller"

func NewWaypointProxyController(client kubelib.Client, clusterID cluster.ID, config func() inject.WebhookConfig, addHandler func(func())) *WaypointProxyController {
	rc := &WaypointProxyController{
		client:       client,
		cluster:      clusterID,
		injectConfig: config,
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			_, err := c.Patch(context.Background(), name, types.ApplyPatchType, data, metav1.PatchOptions{
				Force:        &t,
				FieldManager: waypointFM,
			}, subresources...)
			return err
		},
	}

	rc.queue = controllers.NewQueue("waypoint proxy",
		controllers.WithReconciler(rc.Reconcile),
		controllers.WithMaxAttempts(5))

	gateways := rc.client.GatewayAPIInformer().Gateway().V1alpha2().Gateways()
	rc.gateways = gateways.Lister()
	rc.gatewayInformer = gateways.Informer()
	rc.gatewayInformer.AddEventHandler(controllers.ObjectHandler(rc.queue.AddObject))

	sas := rc.client.KubeInformer().Core().V1().ServiceAccounts()
	rc.serviceAccounts = sas.Lister()
	rc.saInformer = sas.Informer()
	rc.saInformer.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// Anytime SA change, trigger all gateways in the namespace. This could probably be more efficient...
		gws, _ := gateways.Lister().Gateways(o.GetNamespace()).List(klabels.Everything())
		for _, gw := range gws {
			rc.queue.AddObject(gw)
		}
	}))
	// On injection template change, requeue all gateways
	addHandler(func() {
		gws, _ := rc.gateways.List(klabels.Everything())
		for _, gw := range gws {
			rc.queue.AddObject(gw)
		}
	})

	return rc
}

func (rc *WaypointProxyController) Run(stop <-chan struct{}) {
	kubelib.WaitForCacheSync(stop, rc.informerSynced)
	waypointLog.Infof("controller start to run")

	go rc.queue.Run(stop)
	<-stop
}

func (rc *WaypointProxyController) informerSynced() bool {
	return rc.gatewayInformer.HasSynced() && rc.saInformer.HasSynced()
}

func (rc *WaypointProxyController) Reconcile(name types.NamespacedName) error {
	if rc.injectConfig().Values.Struct().GetGlobal().GetHub() == "" {
		// Mostly used to avoid issues with local runs
		return fmt.Errorf("injection config invalid, skipping reconile")
	}
	log := waypointLog.WithLabels("gateway", name.String())

	gw, err := rc.gateways.Gateways(name.Namespace).Get(name.Name)
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

	if gw.Spec.GatewayClassName != "istio-mesh" {
		log.Debugf("mismatched class %q", gw.Spec.GatewayClassName)
		return nil
	}

	wantProxies := sets.New[string]()

	// by default, match all
	gatewaySA := gw.Annotations["istio.io/service-account"]
	serviceAccounts, _ := rc.serviceAccounts.ServiceAccounts(gw.Namespace).List(klabels.Everything())
	for _, sa := range serviceAccounts {
		if gatewaySA != "" && sa.Name != gatewaySA {
			log.Debugf("skip service account %v, doesn't match gateway %v", sa.Name, gatewaySA)
			continue
		}
		wantProxies.Insert(sa.Name)
	}

	for _, sa := range wantProxies.UnsortedList() {
		input := MergedInput{
			Namespace:      gw.Namespace,
			GatewayName:    gw.Name,
			UID:            string(gw.UID),
			ServiceAccount: sa,
			Cluster:        rc.cluster.String(),
			ProxyConfig:    rc.injectConfig().MeshConfig.GetDefaultConfig(),
		}
		proxyDeploy, err := rc.RenderDeploymentApply(input)
		if err != nil {
			return err
		}
		// TODO: should support HPA, PDB, maybe others...
		log.Infof("Updating waypoint proxy %q", sa+"-waypoint-proxy")
		result, err := rc.client.Kube().AppsV1().Deployments(name.Namespace).Apply(context.Background(), proxyDeploy, metav1.ApplyOptions{
			Force: true, FieldManager: waypointFM,
		})
		if err != nil {
			return fmt.Errorf("waypoint deployment patch error: %v", err)
		}
		_ = rc.registerWaypointUpdate(name, result, gw, gatewaySA, log)
	}
	return nil
}

func (rc *WaypointProxyController) registerWaypointUpdate(name types.NamespacedName, proxyDeploy *appsv1.Deployment, gw *v1alpha2.Gateway, gatewaySA string, log *istiolog.Scope) error {
	msg := fmt.Sprintf("Deployed waypoint proxy to %q namespace", gw.Namespace)
	if gatewaySA != "" {
		msg += fmt.Sprintf(" for %q service account", gatewaySA)
	}
	err := rc.UpdateStatus(gw, map[string]*istiogw.Condition{
		string(v1alpha2.GatewayConditionReady): {
			Reason:  string(v1alpha2.GatewayReasonReady),
			Message: msg,
		},
		string(v1alpha2.GatewayConditionAccepted): {
			Reason:  string(v1alpha2.GatewayReasonAccepted),
			Message: msg,
		},
	})
	if err != nil {
		log.Errorf("unable to update Gateway status %v on create: %v", gw.Name, err)
	}
	return nil
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
	if err := rc.ApplyObject(gws, "status"); err != nil {
		return fmt.Errorf("update gateway status: %v", err)
	}
	log.Info("gateway updated")
	return nil
}

// ApplyObject renders an object with the given input and (server-side) applies the results to the cluster.
func (rc *WaypointProxyController) ApplyObject(obj controllers.Object, subresources ...string) error {
	j, err := config.ToJSON(obj)
	if err != nil {
		return err
	}

	gvr, err := controllers.ObjectToGVR(obj)
	if err != nil {
		return err
	}
	log.Debugf("applying %v", string(j))

	return rc.patcher(gvr, obj.GetName(), obj.GetNamespace(), j, subresources...)
}

func (rc *WaypointProxyController) RenderDeploymentApply(input MergedInput) (*appsv1ac.DeploymentApplyConfiguration, error) {
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

	proxyPod, err := unmarshalDeployApply([]byte(waypointBytes))
	if err != nil {
		return nil, fmt.Errorf("render: %v\n%v", err, waypointBytes)
	}
	return proxyPod, nil
}

func unmarshalDeployApply(dyaml []byte) (*appsv1ac.DeploymentApplyConfiguration, error) {
	deploy := &appsv1ac.DeploymentApplyConfiguration{}
	if err := yaml.Unmarshal(dyaml, deploy); err != nil {
		return nil, err
	}

	return deploy, nil
}

type MergedInput struct {
	GatewayName string

	Namespace       string
	UID             string
	ServiceAccount  string
	Cluster         string
	Image           string
	ImagePullPolicy string
	ProxyConfig     *meshapi.ProxyConfig
}
