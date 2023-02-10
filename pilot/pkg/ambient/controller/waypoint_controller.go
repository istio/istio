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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/gateway-api/apis/v1beta1"
	gwlister "sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1beta1"
	"sigs.k8s.io/yaml"

	meshapi "istio.io/api/mesh/v1alpha1"
	istiogw "istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/util/tmpl"
	istiolog "istio.io/pkg/log"
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

var (
	waypointLog = istiolog.RegisterScope("waypointproxy", "", 0)
	waypointFM  = "waypoint proxy controller"
)

func NewWaypointProxyController(client kubelib.Client, clusterID cluster.ID,
	config func() inject.WebhookConfig, addHandler func(func()),
) *WaypointProxyController {
	rc := &WaypointProxyController{
		client:       client,
		cluster:      clusterID,
		injectConfig: config,
		patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
			c := client.Dynamic().Resource(gvr).Namespace(namespace)
			t := true
			_, err := c.Patch(
				context.Background(),
				name,
				types.ApplyPatchType,
				data,
				metav1.PatchOptions{
					Force:        &t,
					FieldManager: waypointFM,
				},
				subresources...)
			return err
		},
	}

	rc.queue = controllers.NewQueue("waypoint proxy",
		controllers.WithReconciler(rc.Reconcile),
		controllers.WithMaxAttempts(5))

	gateways := rc.client.GatewayAPIInformer().Gateway().V1beta1().Gateways()
	rc.gateways = gateways.Lister()
	rc.gatewayInformer = gateways.Informer()
	_, _ = rc.gatewayInformer.AddEventHandler(controllers.ObjectHandler(rc.queue.AddObject))

	sas := rc.client.KubeInformer().Core().V1().ServiceAccounts()
	rc.serviceAccounts = sas.Lister()
	rc.saInformer = sas.Informer()
	_, _ = rc.saInformer.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// Anytime SA change, trigger all gateways in the namespace. This could probably be more efficient...
		gws, _ := rc.gateways.Gateways(o.GetNamespace()).List(klabels.Everything())
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

	// TODO(https://github.com/istio/istio/issues/43264): actually use a real GatewayClass
	if gw.Spec.GatewayClassName != constants.WaypointClass {
		log.Debugf("mismatched class %q", gw.Spec.GatewayClassName)
		return nil
	}

	defaultName := istiogw.GetDefaultGatewayName(gw.Name, &gw.Spec)
	proxyName := defaultName
	if override, exists := gw.Annotations[istiogw.GatewayNameOverride]; exists {
		proxyName = override
	}

	gatewaySA := defaultName
	forSa := gw.Annotations[constants.WaypointServiceAccount]
	if override, exists := gw.Annotations[istiogw.GatewaySAOverride]; exists {
		gatewaySA = override
	}

	input := MergedInput{
		Name:              proxyName,
		Namespace:         gw.Namespace,
		GatewayName:       gw.Name,
		UID:               string(gw.UID),
		ServiceAccount:    gatewaySA,
		Cluster:           rc.cluster.String(),
		ProxyConfig:       rc.injectConfig().MeshConfig.GetDefaultConfig(),
		ForServiceAccount: forSa,
	}
	log.Infof("Updating waypoint proxy %q", proxyName)
	proxySa := rc.RenderServiceAccountApply(input)
	_, err = rc.client.Kube().
		CoreV1().
		ServiceAccounts(name.Namespace).
		Apply(context.Background(), proxySa, metav1.ApplyOptions{
			Force: true, FieldManager: waypointFM,
		})
	if err != nil {
		return fmt.Errorf("waypoint service account patch error: %v", err)
	}

	proxyDeploy, err := rc.RenderDeploymentApply(input)
	if err != nil {
		return err
	}
	// TODO: should support HPA, PDB, maybe others...
	_, err = rc.client.Kube().
		AppsV1().
		Deployments(name.Namespace).
		Apply(context.Background(), proxyDeploy, metav1.ApplyOptions{
			Force: true, FieldManager: waypointFM,
		})
	if err != nil {
		return fmt.Errorf("waypoint deployment patch error: %v", err)
	}
	rc.registerWaypointUpdate(gw, gatewaySA, log)
	return nil
}

func (rc *WaypointProxyController) registerWaypointUpdate(
	gw *v1beta1.Gateway,
	gatewaySA string,
	log *istiolog.Scope,
) {
	msg := fmt.Sprintf("Deployed waypoint proxy to %q namespace", gw.Namespace)
	if gatewaySA != "" {
		msg += fmt.Sprintf(" for %q service account", gatewaySA)
	}
	accept := msg
	if unexpectedListener(gw) {
		accept += "; WARN: expected a single listener on port 15008 with protocol \"ALL\""
	}
	err := rc.UpdateStatus(gw, map[string]*istiogw.Condition{
		string(v1beta1.GatewayConditionReady): {
			Reason:  string(v1beta1.GatewayReasonReady),
			Message: msg,
		},
		string(v1beta1.ListenerConditionProgrammed): {
			Reason:  string(v1beta1.GatewayReasonProgrammed),
			Message: msg,
		},
		string(v1beta1.GatewayConditionAccepted): {
			Reason:  string(v1beta1.GatewayReasonAccepted),
			Message: accept,
		},
	}, log)
	if err != nil {
		log.Errorf("unable to update Gateway status %v on create: %v", gw.Name, err)
	}
}

// Gateway currently requires a listener (https://github.com/kubernetes-sigs/gateway-api/pull/1596).
// We don't *really* care about the listener, but it may make sense to add a warning if users do not
// configure it in an expected way so that we have consistency and can make changes in the future as needed.
// We could completely reject but that seems more likely to cause pain.
func unexpectedListener(gw *v1beta1.Gateway) bool {
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

func (rc *WaypointProxyController) UpdateStatus(
	gw *v1beta1.Gateway,
	conditions map[string]*istiogw.Condition,
	log *istiolog.Scope,
) error {
	if gw == nil {
		return nil
	}
	gws := &v1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.KubernetesGateway.Kind,
			APIVersion: gvk.KubernetesGateway.Group + "/" + gvk.KubernetesGateway.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		},
		Status: v1beta1.GatewayStatus{
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
func (rc *WaypointProxyController) ApplyObject(
	obj controllers.Object,
	subresources ...string,
) error {
	// TODO: use library options when available https://github.com/kubernetes-sigs/gateway-api/issues/1639
	j, err := config.ToJSON(obj)
	if err != nil {
		return err
	}

	gvr, err := controllers.ObjectToGVR(obj)
	if err != nil {
		return err
	}
	waypointLog.Debugf("applying %v", string(j))

	return rc.patcher(gvr, obj.GetName(), obj.GetNamespace(), j, subresources...)
}

func (rc *WaypointProxyController) RenderServiceAccountApply(input MergedInput) *corev1ac.ServiceAccountApplyConfiguration {
	return corev1ac.ServiceAccount(input.ServiceAccount, input.Namespace).
		WithLabels(map[string]string{istiogw.GatewayNameLabel: input.GatewayName}).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithName(input.GatewayName).
			WithUID(types.UID(input.UID)).
			WithKind(gvk.KubernetesGateway.Kind).
			WithAPIVersion(gvk.KubernetesGateway.GroupVersion()))
}

func (rc *WaypointProxyController) RenderDeploymentApply(
	input MergedInput,
) (*appsv1ac.DeploymentApplyConfiguration, error) {
	cfg := rc.injectConfig()

	// TODO watch for template changes, update the Deployment if it does
	podTemplate := cfg.Templates["waypoint"]
	if podTemplate == nil {
		return nil, fmt.Errorf("no waypoint template defined")
	}
	input.Image = inject.ProxyImage(
		cfg.Values.Struct(),
		cfg.MeshConfig.GetDefaultConfig().GetImage(),
		nil,
	)
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
	GatewayName       string
	Name              string
	Namespace         string
	UID               string
	ServiceAccount    string
	Cluster           string
	Image             string
	ImagePullPolicy   string
	ProxyConfig       *meshapi.ProxyConfig
	ForServiceAccount string
}
