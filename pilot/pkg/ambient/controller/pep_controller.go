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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
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

type RemoteProxyController struct {
	client          kubelib.Client
	queue           controllers.Queue
	pods            listerv1.PodLister
	serviceAccounts listerv1.ServiceAccountLister
	gateways        v1alpha2.GatewayLister

	cluster cluster.ID

	injectConfig func() inject.WebhookConfig
}

var remoteLog = istiolog.RegisterScope("remote proxy", "", 0)

func init() {
	remoteLog.SetOutputLevel(istiolog.DebugLevel)
}

func NewRemoteProxyController(client kubelib.Client, clusterID cluster.ID, config func() inject.WebhookConfig) *RemoteProxyController {
	rc := &RemoteProxyController{
		client:       client,
		cluster:      clusterID,
		injectConfig: config,
	}

	rc.queue = controllers.NewQueue("remote proxy",
		controllers.WithReconciler(rc.Reconcile),
		controllers.WithMaxAttempts(5))

	gateways := rc.client.GatewayAPIInformer().Gateway().V1alpha2().Gateways()
	rc.gateways = gateways.Lister()
	gateways.Informer().AddEventHandler(controllers.ObjectHandler(rc.queue.AddObject))

	pods := rc.client.KubeInformer().Core().V1().Pods()
	rc.pods = pods.Lister()
	pods.Informer().AddEventHandler(controllers.ObjectHandler(controllers.EnqueueForParentHandler(rc.queue, gvk.KubernetesGateway)))

	sas := rc.client.KubeInformer().Core().V1().ServiceAccounts()
	rc.serviceAccounts = sas.Lister()
	sas.Informer().AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// Anytime SA change, trigger all gateways in the namespace. This could probably be more efficient...
		gws, _ := gateways.Lister().Gateways(o.GetNamespace()).List(klabels.Everything())
		for _, gw := range gws {
			rc.queue.AddObject(gw)
		}
	}))

	return rc
}

func (rc *RemoteProxyController) Run(stop <-chan struct{}) {
	go rc.queue.Run(stop)
	<-stop
}

func (rc *RemoteProxyController) proxiesForWorkload(pod *v1.Pod) []string {
	if pod.Labels["asm-proxy"] != "" {
		// Remote proxy
		return nil
	}
	if pod.Labels["asm-type"] != "workload" {
		// Not in mesh
		return nil
	}
	var ips []string
	proxyLbl, _ := klabels.Parse("gateway.istio.io/managed=istio.io-mesh-controller")
	proxyPods, _ := rc.pods.Pods(pod.Namespace).List(proxyLbl)
	for _, p := range proxyPods {
		if !kubecontroller.IsPodReady(p) {
			continue
		}
		if p.Spec.ServiceAccountName == pod.Spec.ServiceAccountName {
			ips = append(ips, p.Status.PodIP)
		}
	}
	return ips
}

func (rc *RemoteProxyController) Reconcile(name types.NamespacedName) error {
	if rc.injectConfig().Values.Struct().GetGlobal().GetHub() == "" {
		// Mostly used to avoid issues with local runs
		return fmt.Errorf("injection config invalid, skipping reconile")
	}
	log := remoteLog.WithLabels("gateway", name.String())

	gw, err := rc.gateways.Gateways(name.Namespace).Get(name.Name)
	if err != nil || gw == nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		if err := controllers.IgnoreNotFound(err); err != nil {
			log.Errorf("unable to fetch Gateway: %v", err)
			return err
		}
		log.Debugf("gateway deleted")
		return rc.pruneGateway(name)
	}

	if gw.Spec.GatewayClassName != "istio-mesh" {
		log.Debugf("mismatched class %q", gw.Spec.GatewayClassName)
		return rc.pruneGateway(name)
	}

	haveProxies := sets.New()
	wantProxies := sets.New()

	proxyLbl, _ := klabels.Parse("gateway.istio.io/managed=istio.io-mesh-controller,istio.io/gateway-name=" + gw.Name)
	proxyPods, _ := rc.pods.Pods(gw.Namespace).List(proxyLbl)
	for _, p := range proxyPods {
		haveProxies.Insert(p.Spec.ServiceAccountName)
	}
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

	add, remove := wantProxies.Diff(haveProxies)

	if len(remove)+len(add) == 0 {
		log.Debugf("reconcile: remove %d, add %d. Have %d", len(remove), len(add), len(haveProxies))
	} else {
		log.Infof("reconcile: remove %d, add %d. Have %d", len(remove), len(add), len(haveProxies))
	}
	for _, k := range remove {
		log.Infof("removing proxy %q", k+"-proxy")
		if err := rc.client.Kube().CoreV1().Pods(gw.Namespace).Delete(context.Background(), k+"-proxy", metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("pod remove: %v", err)
		}
	}
	for _, k := range add {
		log.Infof("adding proxy %v", k+"-proxy")
		input := MergedInput{
			Namespace:      gw.Namespace,
			GatewayName:    gw.Name,
			UID:            string(gw.UID),
			ServiceAccount: k,
			Cluster:        rc.cluster.String(),
		}
		proxyPod, err := rc.RenderPodMerged(input)
		if err != nil {
			return err
		}

		if _, err := rc.client.Kube().CoreV1().Pods(name.Namespace).Create(context.Background(), proxyPod, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("pod create: %v", err)
		}
	}
	return nil
}

func (rc *RemoteProxyController) RenderPodMerged(input MergedInput) (*v1.Pod, error) {
	cfg := rc.injectConfig()

	podTemplate := cfg.Templates["remote"]
	if podTemplate == nil {
		return nil, fmt.Errorf("no remote template defined")
	}
	input.Image = inject.ProxyImage(cfg.Values.Struct(), cfg.MeshConfig.GetDefaultConfig().GetImage(), nil)
	remoteBytes, err := tmpl.Execute(podTemplate, input)
	if err != nil {
		return nil, err
	}
	proxyPod, err := unmarshalPod([]byte(remoteBytes))
	if err != nil {
		return nil, fmt.Errorf("render: %v\n%v", err, remoteBytes)
	}
	return proxyPod, nil
}

// pruneGateway removes all proxies for the given Gateway
// This is not super required since Kubernetes GC can do it as well
func (rc *RemoteProxyController) pruneGateway(gw types.NamespacedName) error {
	proxyLbl, _ := klabels.Parse("gateway.istio.io/managed=istio.io-mesh-controller,istio.io/gateway-name=" + gw.Name)
	proxyPods, _ := rc.pods.Pods(gw.Namespace).List(proxyLbl)
	for _, p := range proxyPods {
		log.Infof("pruning proxy %v", p.Name)
		if err := rc.client.Kube().CoreV1().Pods(gw.Namespace).Delete(context.Background(), p.Spec.ServiceAccountName+"-proxy", metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("pod remove: %v", err)
		}
	}
	return nil
}

func unmarshalPod(pyaml []byte) (*v1.Pod, error) {
	pod := &v1.Pod{}
	if err := yaml.Unmarshal(pyaml, pod); err != nil {
		return nil, err
	}

	return pod, nil
}

type MergedInput struct {
	GatewayName string

	Namespace      string
	UID            string
	ServiceAccount string
	Cluster        string
	Image          string
}
