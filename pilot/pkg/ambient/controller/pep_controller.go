package controller

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

import (
	"context"
	"istio.io/istio/pilot/pkg/model"
	"sigs.k8s.io/yaml"
	"sort"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	listerv1 "k8s.io/client-go/listers/core/v1"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/pkg/env"
)

type proxy struct {
	Name           string `json:"name,omitempty"`
	IP             string `json:"ip,omitempty"`
	ServiceAccount string `json:"serviceAccount"`
}

type pepController struct {
	*Options
	pods listerv1.PodLister

	proxiesMu    sync.Mutex
	injectConfig func() inject.WebhookConfig
}

func initPEPController(opts *Options) *pepController {
	// TODO support for deploying peps into a remote cluster
	pods := opts.Client.KubeInformer().Core().V1().Pods()
	pc := &pepController{
		Options:      opts,
		pods:         pods.Lister(),
		injectConfig: opts.WebhookConfig,
	}
	proxyQueue := controllers.NewQueue("pep deployer",
		controllers.WithReconciler(func(key types.NamespacedName) error {
			return pc.Reconcile(key)
		}),
		controllers.WithMaxAttempts(5),
	)
	proxyHandler := controllers.FilteredObjectHandler(enqueuePEP(proxyQueue))
	pods.Informer().AddEventHandler(proxyHandler)
	go proxyQueue.Run(pc.Stop)
	return pc
}

func enqueuePEP(q controllers.Queue) (func(obj controllers.Object), func(o controllers.Object) bool) {
	handler := func(obj controllers.Object) {
		if obj.GetLabels()[ambient.LabelType] == ambient.TypePEP {
			// This is the proxy, enqueue it directly
			q.AddObject(obj)
			return
		}
		// Enqueue the proxy. Note the filter makes sure this only applies to pods we care about
		q.Add(types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName() + "-proxy",
		})
	}
	filter := func(o controllers.Object) bool {
		isPEP := o.GetLabels()[ambient.LabelType] == ambient.TypePEP
		wantsPEP, _ := strconv.ParseBool(o.GetLabels()[ambient.LabelPEP])
		return isPEP || wantsPEP
	}
	return handler, filter
}

// mergeProxies, if true, means we merge PEPs by service account
var mergeProxies = env.RegisterBoolVar("AMBIENT_MERGE_PROXIES", true, "").Get()

type ProxySpecifier struct {
	Namespace      string `json:"namespace,omitempty"`
	ServiceAccount string `json:"serviceAccount,omitempty"`
	// TODO: "config set" determined by workload labels
}

func ProxySpecifierForPod(p *corev1.Pod) ProxySpecifier {
	return ProxySpecifier{
		Namespace:      p.GetNamespace(),
		ServiceAccount: p.Spec.ServiceAccountName,
	}
}

func (pc *pepController) Reconcile(name types.NamespacedName) error {
	log := log.WithLabels("pod", name.String())
	if mergeProxies {
		return pc.reconcileMerged(name)
	}
	proxy, _ := pc.pods.Pods(name.Namespace).Get(name.Name)
	haveProxy := proxy != nil
	workload, _ := pc.pods.Pods(name.Namespace).Get(strings.TrimSuffix(name.Name, "-proxy"))
	wantProxy := workload != nil && workload.GetLabels()[ambient.LabelPEP] == "true"
	if haveProxy && wantProxy {
		log.Infof("Proxy already exists")
		return nil
		// TODO: update it, etc. Likely can do blue/green pretty well
	}
	if !haveProxy && !wantProxy {
		log.Infof("Proxy not requested")
		return nil
	}

	if haveProxy && !wantProxy {
		log.Infof("Proxy needs to be removed")
		return pc.Client.CoreV1().Pods(name.Namespace).Delete(context.Background(), name.Name, metav1.DeleteOptions{})
	}
	if !haveProxy && wantProxy {
		log.Infof("Proxy needs to be created")
		input := TemplateInput{
			Pod:            workload,
			ProxyName:      workload.Name,
			ServiceAccount: workload.Spec.ServiceAccountName,
			Cluster:        pc.ClusterID.String(),
		}
		proxyPod, err := pc.RenderPod(input)
		if err != nil {
			return err
		}
		if _, err := pc.Client.CoreV1().Pods(name.Namespace).Create(context.Background(), proxyPod, metav1.CreateOptions{}); err != nil {
			return err
		}
		return nil
	}
	panic("Not reachable")
}
func (pc *pepController) RenderPod(input TemplateInput) (*corev1.Pod, error) {
	cfg := pc.injectConfig()
	input.Image = inject.ProxyImage(cfg.Values.Struct(), cfg.MeshConfig.GetDefaultConfig().GetImage(), nil)
	remoteBytes, err := tmpl.Evaluate(podTemplate, input)
	if err != nil {
		return nil, err
	}
	proxyPod, err := unmarshalPod([]byte(remoteBytes))
	if err != nil {
		return nil, err
	}
	return proxyPod, nil
}
func (pc *pepController) reconcileMerged(name types.NamespacedName) error {
	log := log.WithLabels("pod", name.String())

	haveProxies := map[ProxySpecifier][]proxy{}
	wantProxies := map[ProxySpecifier][]proxy{}
	proxyLbl, _ := klabels.Parse(ambient.LabelProxy)
	proxyPods, _ := pc.pods.List(proxyLbl)
	for _, p := range proxyPods {
		sp := ProxySpecifierForPod(p)
		p := proxy{Name: p.Name, IP: p.Status.PodIP, ServiceAccount: p.Spec.ServiceAccountName}
		haveProxies[sp] = append(haveProxies[sp], p)
	}
	for _, ps := range haveProxies {
		sort.Slice(ps, func(i, j int) bool {
			return ps[i].Name < ps[j].Name
		})
	}

	needProxyLbl, _ := klabels.Parse(ambient.LabelPEP + "=true")
	workloadPods, _ := pc.pods.List(needProxyLbl)
	for _, p := range workloadPods {
		sp := ProxySpecifierForPod(p)
		p := proxy{
			Name:           p.Spec.ServiceAccountName + "-proxy",
			ServiceAccount: p.Spec.ServiceAccountName,
		}
		wantProxies[sp] = append(wantProxies[sp], p)
	}
	for _, ps := range wantProxies {
		sort.Slice(ps, func(i, j int) bool {
			return ps[i].Name < ps[j].Name
		})
	}
	add, remove := difference(haveProxies, wantProxies)

	for k, vs := range remove {
		for _, v := range vs {
			log.Infof("removing proxy %v/%v", k.Namespace, v.Name)
			if err := pc.Client.CoreV1().Pods(k.Namespace).Delete(context.TODO(), v.Name, metav1.DeleteOptions{}); err != nil {
				return err
			}
		}
	}
	for k, vs := range add {
		for _, v := range vs {
			log.Infof("adding proxy %v/%v", k.Namespace, v.Name)
			input := MergedInput{
				Namespace:      k.Namespace,
				ProxyName:      v.Name,
				ServiceAccount: k.ServiceAccount,
				Cluster:        pc.ClusterID.String(),
			}
			proxyPod, err := pc.RenderPodMerged(input)
			if err != nil {
				return err
			}
			// Create NS if it doesn't already exist
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: name.Namespace},
			}
			if _, err := pc.Client.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
			// Create SA if it doesn't already exist
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyPod.Spec.ServiceAccountName,
					Namespace: name.Namespace,
				},
			}
			if _, err := pc.Client.CoreV1().ServiceAccounts(name.Namespace).Create(context.Background(), sa, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
			if _, err := pc.Client.CoreV1().Pods(name.Namespace).Create(context.Background(), proxyPod, metav1.CreateOptions{}); err != nil {
				return err
			}
			return nil
		}
	}

	// TODO be smarter about pushes
	pc.xds.ConfigUpdate(&model.PushRequest{Full: true})
	return nil
}

func (pc *pepController) RenderPodMerged(input MergedInput) (*corev1.Pod, error) {
	cfg := pc.injectConfig()
	input.Image = inject.ProxyImage(cfg.Values.Struct(), cfg.MeshConfig.GetDefaultConfig().GetImage(), nil)
	remoteBytes, err := tmpl.Evaluate(podTemplate, input)
	if err != nil {
		return nil, err
	}
	proxyPod, err := unmarshalPod([]byte(remoteBytes))
	if err != nil {
		return nil, err
	}
	return proxyPod, nil
}

func proxyListDifference(have []proxy, want []proxy) (add, delete []proxy) {
	both := map[proxy]struct{}{}
	hm := map[proxy]struct{}{}
	for _, h := range have {
		h.IP = "" // ignore IP
		hm[h] = struct{}{}
		both[h] = struct{}{}
	}
	wm := map[proxy]struct{}{}
	for _, h := range want {
		wm[h] = struct{}{}
		both[h] = struct{}{}
	}
	for k := range both {
		_, hf := hm[k]
		_, wf := wm[k]
		if hf && wf {
			continue
		}
		if hf {
			delete = append(delete, k)
		}
		if wf {
			add = append(add, k)
		}
	}
	return nil, nil
}

func difference(have map[ProxySpecifier][]proxy, want map[ProxySpecifier][]proxy) (add, del map[ProxySpecifier][]proxy) {
	add = map[ProxySpecifier][]proxy{}
	del = map[ProxySpecifier][]proxy{}
	for k, v := range have {
		if _, f := want[k]; !f {
			// We don't want this specifier at all, wipe it all out
			del[k] = v
			continue
		}
		padd, pdel := proxyListDifference(v, want[k])
		if len(padd) > 0 {
			add[k] = padd
		}
		if len(pdel) > 0 {
			del[k] = pdel
		}
	}
	for k, v := range want {
		if _, f := have[k]; !f {
			// We don't have this specifier at all, add it
			add[k] = v
			continue
		}
		padd, pdel := proxyListDifference(v, have[k])
		if len(padd) > 0 {
			add[k] = padd
		}
		if len(pdel) > 0 {
			del[k] = pdel
		}
	}
	return add, del
}

func unmarshalPod(pyaml []byte) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(pyaml, pod); err != nil {
		return nil, err
	}
	return pod, nil
}

type MergedInput struct {
	Namespace      string
	ProxyName      string
	ServiceAccount string
	SplitClusters  bool
	Cluster        string
	Image          string
}
type TemplateInput struct {
	*corev1.Pod
	ProxyName      string
	ServiceAccount string
	SplitClusters  bool
	Cluster        string
	Image          string
}

var podTemplate = `
apiVersion: v1
kind: Pod
metadata:
  name: {{.ProxyName}}
  namespace: {{.Namespace}}
  annotations:
    prometheus.io/path: /stats/prometheus
    prometheus.io/port: "15020"
    prometheus.io/scrape: "true"
  labels:
    ambient-proxy: {{.ProxyName}}
    ambient-type: pep
spec:
  nodeSelector:
    kubernetes.io/hostname: ambient-worker # REMOVE ME
  terminationGracePeriodSeconds: 2
  serviceAccountName: {{.ServiceAccount}}
  initContainers:
  - name: istio-init
    image: {{.Image}}
    securityContext:
      privileged: true
      capabilities:
        add:
        - NET_ADMIN
        - NET_RAW
    command:
      - sh
      - -c
      - |
        iptables -t nat -A OUTPUT -p tcp -m owner --uid-owner 1337 -s 127.0.0.6/32 -j REDIRECT --to-ports 15002
        iptables -t nat -A OUTPUT -p tcp -m owner --uid-owner 1337 -j REDIRECT --to-ports 15002
    env:
    - name: INSTANCE_IP
      valueFrom:
        fieldRef:
          fieldPath: status.podIP
  containers:
  - args:
    - proxy
    - sidecar
    - --domain
    - $(POD_NAMESPACE).svc.cluster.local
    - --serviceCluster
    - proxy.$(POD_NAMESPACE)
    - --proxyLogLevel=warning
    - --proxyComponentLogLevel=misc:error
    - --trust-domain=cluster.local
    - --concurrency
    - "2"
    env:
    - name: ISTIO_META_GENERATOR
      value: "ambient-pep"
    - name: ISTIO_META_SIDECARLESS_TYPE
      value: "pep"
    - name: NODE_NAME
      valueFrom:
        fieldRef:
          fieldPath: spec.nodeName
    - name: CREDENTIAL_FETCHER_TYPE
      value: ""
    - name: JWT_POLICY
      value: third-party-jwt
    - name: PILOT_CERT_PROVIDER
      value: istiod
    - name: POD_NAME
      valueFrom:
        fieldRef:
          fieldPath: metadata.name
    - name: POD_NAMESPACE
      valueFrom:
        fieldRef:
          fieldPath: metadata.namespace
    - name: INSTANCE_IP
      valueFrom:
        fieldRef:
          fieldPath: status.podIP
    - name: SERVICE_ACCOUNT
      valueFrom:
        fieldRef:
          fieldPath: spec.serviceAccountName
    - name: HOST_IP
      valueFrom:
        fieldRef:
          fieldPath: status.hostIP
    - name: CANONICAL_SERVICE
      valueFrom:
        fieldRef:
          fieldPath: metadata.labels['service.istio.io/canonical-name']
    - name: CANONICAL_REVISION
      valueFrom:
        fieldRef:
          fieldPath: metadata.labels['service.istio.io/canonical-revision']
    - name: ISTIO_META_POD_PORTS
      value: |-
        [
        ]
    - name: ISTIO_META_APP_CONTAINERS
      value: proxy
    - name: ISTIO_META_CLUSTER_ID
      value: {{.Cluster}}
    - name: ISTIO_META_INTERCEPTION_MODE
      value: REDIRECT
    - name: ISTIO_META_WORKLOAD_NAME
      value: proxy
    - name: ISTIO_META_OWNER
      value: kubernetes://apis/apps/v1/namespaces/default/deployments/proxy
    - name: ISTIO_META_MESH_ID
      value: cluster.local
    image: {{.Image}}
    imagePullPolicy: Always
    name: istio-proxy
    resources:
      limits:
        cpu: "2"
        memory: 1Gi
      requests:
        cpu: 100m
        memory: 128Mi
    readinessProbe:
      failureThreshold: 30
      httpGet:
        path: /healthz/ready
        port: 15021
        scheme: HTTP
      initialDelaySeconds: 1
      periodSeconds: 2
      successThreshold: 1
      timeoutSeconds: 1
    securityContext:
      privileged: true
      runAsGroup: 1337
      capabilities:
        add:
        - NET_ADMIN
        - NET_RAW
    volumeMounts:
    - mountPath: /var/run/secrets/istio
      name: istiod-ca-cert
    - mountPath: /var/lib/istio/data
      name: istio-data
    - mountPath: /etc/istio/proxy
      name: istio-envoy
    - mountPath: /var/run/secrets/tokens
      name: istio-token
    - mountPath: /etc/istio/pod
      name: istio-podinfo
  volumes:
  - emptyDir:
      medium: Memory
    name: istio-envoy
  - emptyDir:
      medium: Memory
    name: go-proxy-envoy
  - emptyDir: {}
    name: istio-data
  - emptyDir: {}
    name: go-proxy-data
  - downwardAPI:
      items:
      - fieldRef:
          fieldPath: metadata.labels
        path: labels
      - fieldRef:
          fieldPath: metadata.annotations
        path: annotations
    name: istio-podinfo
  - name: istio-token
    projected:
      sources:
      - serviceAccountToken:
          audience: istio-ca
          expirationSeconds: 43200
          path: istio-token
  - configMap:
      name: istio-ca-root-cert
    name: istiod-ca-cert
`
