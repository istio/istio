// Copyright 2019 Istio Authors
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

package mock

import (
	"fmt"
	"sync"

	apicorev1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	admissionregistrationv1alpha1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1alpha1"
	admissionregistrationv1beta1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	appsv1beta1 "k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	appsv1beta2 "k8s.io/client-go/kubernetes/typed/apps/v1beta2"
	authenticationv1 "k8s.io/client-go/kubernetes/typed/authentication/v1"
	authenticationv1beta1 "k8s.io/client-go/kubernetes/typed/authentication/v1beta1"
	authorizationv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
	authorizationv1beta1 "k8s.io/client-go/kubernetes/typed/authorization/v1beta1"
	autoscalingv1 "k8s.io/client-go/kubernetes/typed/autoscaling/v1"
	autoscalingv2beta1 "k8s.io/client-go/kubernetes/typed/autoscaling/v2beta1"
	batchv1 "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchv1beta1 "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	batchv2alpha1 "k8s.io/client-go/kubernetes/typed/batch/v2alpha1"
	certificatesv1beta1 "k8s.io/client-go/kubernetes/typed/certificates/v1beta1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	eventsv1beta1 "k8s.io/client-go/kubernetes/typed/events/v1beta1"
	extensionsv1beta1 "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	networkingv1 "k8s.io/client-go/kubernetes/typed/networking/v1"
	policyv1beta1 "k8s.io/client-go/kubernetes/typed/policy/v1beta1"
	rbacv1 "k8s.io/client-go/kubernetes/typed/rbac/v1"
	rbacv1alpha1 "k8s.io/client-go/kubernetes/typed/rbac/v1alpha1"
	rbacv1beta1 "k8s.io/client-go/kubernetes/typed/rbac/v1beta1"
	schedulingv1alpha1 "k8s.io/client-go/kubernetes/typed/scheduling/v1alpha1"
	schedulingv1beta1 "k8s.io/client-go/kubernetes/typed/scheduling/v1beta1"
	settingsv1alpha1 "k8s.io/client-go/kubernetes/typed/settings/v1alpha1"
	storagev1 "k8s.io/client-go/kubernetes/typed/storage/v1"
	storagev1alpha1 "k8s.io/client-go/kubernetes/typed/storage/v1alpha1"
	storagev1beta1 "k8s.io/client-go/kubernetes/typed/storage/v1beta1"
	"k8s.io/client-go/rest"
)

var _ kubernetes.Interface = &kubeClient{}

type kubeClient struct {
	core corev1.CoreV1Interface
}

// NewKubeClient returns a lightweight fake that implements kubernetes.Interface. Only implements a portion of the
// interface that is used by galley tests. The clientset provided in the kubernetes/fake package has proven poor
// for benchmarking because of the amount of garbage created and spurious channel full panics due to the fact that
// the fake watch channels are sized at 100 items.
func NewKubeClient() kubernetes.Interface {
	return &kubeClient{
		core: &corev1Impl{
			nodes:     newNodeInterface(),
			pods:      newPodInterface(),
			services:  newServiceInterface(),
			endpoints: newEndpointsInterface(),
		},
	}
}

func (c *kubeClient) CoreV1() corev1.CoreV1Interface {
	return c.core
}

var _ corev1.CoreV1Interface = &corev1Impl{}

type corev1Impl struct {
	nodes     corev1.NodeInterface
	pods      corev1.PodInterface
	services  corev1.ServiceInterface
	endpoints corev1.EndpointsInterface
}

func (c *corev1Impl) Nodes() corev1.NodeInterface {
	return c.nodes
}

func (c *corev1Impl) Pods(namespace string) corev1.PodInterface {
	return c.pods
}

func (c *corev1Impl) Services(namespace string) corev1.ServiceInterface {
	return c.services
}

func (c *corev1Impl) Endpoints(namespace string) corev1.EndpointsInterface {
	return c.endpoints
}

var _ corev1.NodeInterface = &nodeImpl{}

type nodeImpl struct {
	mux     sync.Mutex
	nodes   map[string]*apicorev1.Node
	watches Watches
}

func newNodeInterface() corev1.NodeInterface {
	return &nodeImpl{
		nodes: make(map[string]*apicorev1.Node),
	}
}

func (n *nodeImpl) Create(obj *apicorev1.Node) (*apicorev1.Node, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	n.nodes[obj.Name] = obj

	n.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (n *nodeImpl) Update(obj *apicorev1.Node) (*apicorev1.Node, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	n.nodes[obj.Name] = obj

	n.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (n *nodeImpl) Delete(name string, options *metav1.DeleteOptions) error {
	n.mux.Lock()
	defer n.mux.Unlock()

	obj := n.nodes[name]
	if obj == nil {
		return fmt.Errorf("unable to delete node %s", name)
	}

	delete(n.nodes, name)

	n.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (n *nodeImpl) List(opts metav1.ListOptions) (*apicorev1.NodeList, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	out := &apicorev1.NodeList{}

	for _, v := range n.nodes {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (n *nodeImpl) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	n.mux.Lock()
	defer n.mux.Unlock()

	w := NewWatch()
	n.watches = append(n.watches, w)

	// Send add events for all current resources.
	for _, node := range n.nodes {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: node,
		})
	}

	return w, nil
}

var _ corev1.PodInterface = &podImpl{}

type podImpl struct {
	mux     sync.Mutex
	pods    map[string]*apicorev1.Pod
	watches Watches
}

func newPodInterface() corev1.PodInterface {
	return &podImpl{
		pods: make(map[string]*apicorev1.Pod),
	}
}

func (p *podImpl) Create(obj *apicorev1.Pod) (*apicorev1.Pod, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.pods[obj.Name] = obj

	p.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (p *podImpl) Update(obj *apicorev1.Pod) (*apicorev1.Pod, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.pods[obj.Name] = obj

	p.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (p *podImpl) Delete(name string, options *metav1.DeleteOptions) error {
	p.mux.Lock()
	defer p.mux.Unlock()

	obj := p.pods[name]
	if obj == nil {
		return fmt.Errorf("unable to delete pod %s", name)
	}

	delete(p.pods, name)

	p.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (p *podImpl) List(opts metav1.ListOptions) (*apicorev1.PodList, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	out := &apicorev1.PodList{}

	for _, v := range p.pods {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (p *podImpl) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	w := NewWatch()
	p.watches = append(p.watches, w)

	// Send add events for all current resources.
	for _, pod := range p.pods {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: pod,
		})
	}

	return w, nil
}

var _ corev1.ServiceInterface = &serviceImpl{}

type serviceImpl struct {
	mux      sync.Mutex
	services map[string]*apicorev1.Service
	watches  Watches
}

func newServiceInterface() corev1.ServiceInterface {
	return &serviceImpl{
		services: make(map[string]*apicorev1.Service),
	}
}

func (s *serviceImpl) Create(obj *apicorev1.Service) (*apicorev1.Service, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.services[obj.Name] = obj

	s.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (s *serviceImpl) Update(obj *apicorev1.Service) (*apicorev1.Service, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.services[obj.Name] = obj

	s.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (s *serviceImpl) Delete(name string, options *metav1.DeleteOptions) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	obj := s.services[name]
	if obj == nil {
		return fmt.Errorf("unable to delete service %s", name)
	}

	delete(s.services, name)

	s.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (s *serviceImpl) List(opts metav1.ListOptions) (*apicorev1.ServiceList, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	out := &apicorev1.ServiceList{}

	for _, v := range s.services {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (s *serviceImpl) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	w := NewWatch()
	s.watches = append(s.watches, w)

	// Send add events for all current resources.
	for _, pod := range s.services {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: pod,
		})
	}

	return w, nil
}

var _ corev1.EndpointsInterface = &endpointsImpl{}

type endpointsImpl struct {
	mux       sync.Mutex
	endpoints map[string]*apicorev1.Endpoints
	watches   Watches
}

func newEndpointsInterface() corev1.EndpointsInterface {
	return &endpointsImpl{
		endpoints: make(map[string]*apicorev1.Endpoints),
	}
}

func (e *endpointsImpl) Create(obj *apicorev1.Endpoints) (*apicorev1.Endpoints, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	e.endpoints[obj.Name] = obj

	e.watches.Send(watch.Event{
		Type:   watch.Added,
		Object: obj,
	})
	return obj, nil
}

func (e *endpointsImpl) Update(obj *apicorev1.Endpoints) (*apicorev1.Endpoints, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	e.endpoints[obj.Name] = obj

	e.watches.Send(watch.Event{
		Type:   watch.Modified,
		Object: obj,
	})
	return obj, nil
}

func (e *endpointsImpl) Delete(name string, options *metav1.DeleteOptions) error {
	e.mux.Lock()
	defer e.mux.Unlock()

	obj := e.endpoints[name]
	if obj == nil {
		return fmt.Errorf("unable to delete endpoints %s", name)
	}

	delete(e.endpoints, name)

	e.watches.Send(watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return nil
}

func (e *endpointsImpl) List(opts metav1.ListOptions) (*apicorev1.EndpointsList, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	out := &apicorev1.EndpointsList{}

	for _, v := range e.endpoints {
		out.Items = append(out.Items, *v)
	}

	return out, nil
}

func (e *endpointsImpl) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	w := NewWatch()
	e.watches = append(e.watches, w)

	// Send add events for all current resources.
	for _, pod := range e.endpoints {
		w.Send(watch.Event{
			Type:   watch.Added,
			Object: pod,
		})
	}

	return w, nil
}

func (c *kubeClient) Discovery() discovery.DiscoveryInterface {
	panic("not implemented")
}

func (c *kubeClient) AdmissionregistrationV1alpha1() admissionregistrationv1alpha1.AdmissionregistrationV1alpha1Interface {
	panic("not implemented")
}

func (c *kubeClient) AdmissionregistrationV1beta1() admissionregistrationv1beta1.AdmissionregistrationV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) Admissionregistration() admissionregistrationv1beta1.AdmissionregistrationV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) AppsV1beta1() appsv1beta1.AppsV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) AppsV1beta2() appsv1beta2.AppsV1beta2Interface {
	panic("not implemented")
}

func (c *kubeClient) AppsV1() appsv1.AppsV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Apps() appsv1.AppsV1Interface {
	panic("not implemented")
}

func (c *kubeClient) AuthenticationV1() authenticationv1.AuthenticationV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Authentication() authenticationv1.AuthenticationV1Interface {
	panic("not implemented")
}

func (c *kubeClient) AuthenticationV1beta1() authenticationv1beta1.AuthenticationV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) AuthorizationV1() authorizationv1.AuthorizationV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Authorization() authorizationv1.AuthorizationV1Interface {
	panic("not implemented")
}

func (c *kubeClient) AuthorizationV1beta1() authorizationv1beta1.AuthorizationV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) AutoscalingV1() autoscalingv1.AutoscalingV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Autoscaling() autoscalingv1.AutoscalingV1Interface {
	panic("not implemented")
}

func (c *kubeClient) AutoscalingV2beta1() autoscalingv2beta1.AutoscalingV2beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) BatchV1() batchv1.BatchV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Batch() batchv1.BatchV1Interface {
	panic("not implemented")
}

func (c *kubeClient) BatchV1beta1() batchv1beta1.BatchV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) BatchV2alpha1() batchv2alpha1.BatchV2alpha1Interface {
	panic("not implemented")
}

func (c *kubeClient) CertificatesV1beta1() certificatesv1beta1.CertificatesV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) Certificates() certificatesv1beta1.CertificatesV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) Core() corev1.CoreV1Interface {
	panic("not implemented")
}

func (c *kubeClient) EventsV1beta1() eventsv1beta1.EventsV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) Events() eventsv1beta1.EventsV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) ExtensionsV1beta1() extensionsv1beta1.ExtensionsV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) Extensions() extensionsv1beta1.ExtensionsV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) NetworkingV1() networkingv1.NetworkingV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Networking() networkingv1.NetworkingV1Interface {
	panic("not implemented")
}

func (c *kubeClient) PolicyV1beta1() policyv1beta1.PolicyV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) Policy() policyv1beta1.PolicyV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) RbacV1() rbacv1.RbacV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Rbac() rbacv1.RbacV1Interface {
	panic("not implemented")
}

func (c *kubeClient) RbacV1beta1() rbacv1beta1.RbacV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) RbacV1alpha1() rbacv1alpha1.RbacV1alpha1Interface {
	panic("not implemented")
}

func (c *kubeClient) SchedulingV1alpha1() schedulingv1alpha1.SchedulingV1alpha1Interface {
	panic("not implemented")
}

func (c *kubeClient) SchedulingV1beta1() schedulingv1beta1.SchedulingV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) Scheduling() schedulingv1beta1.SchedulingV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) SettingsV1alpha1() settingsv1alpha1.SettingsV1alpha1Interface {
	panic("not implemented")
}

func (c *kubeClient) Settings() settingsv1alpha1.SettingsV1alpha1Interface {
	panic("not implemented")
}

func (c *kubeClient) StorageV1beta1() storagev1beta1.StorageV1beta1Interface {
	panic("not implemented")
}

func (c *kubeClient) StorageV1() storagev1.StorageV1Interface {
	panic("not implemented")
}

func (c *kubeClient) Storage() storagev1.StorageV1Interface {
	panic("not implemented")
}

func (c *kubeClient) StorageV1alpha1() storagev1alpha1.StorageV1alpha1Interface {
	panic("not implemented")
}

func (c *corev1Impl) RESTClient() rest.Interface {
	panic("not implemented")
}

func (c *corev1Impl) ComponentStatuses() corev1.ComponentStatusInterface {
	panic("not implemented")
}

func (c *corev1Impl) ConfigMaps(namespace string) corev1.ConfigMapInterface {
	panic("not implemented")
}

func (c *corev1Impl) Events(namespace string) corev1.EventInterface {
	panic("not implemented")
}

func (c *corev1Impl) LimitRanges(namespace string) corev1.LimitRangeInterface {
	panic("not implemented")
}

func (c *corev1Impl) Namespaces() corev1.NamespaceInterface {
	panic("not implemented")
}

func (c *corev1Impl) PersistentVolumes() corev1.PersistentVolumeInterface {
	panic("not implemented")
}

func (c *corev1Impl) PersistentVolumeClaims(namespace string) corev1.PersistentVolumeClaimInterface {
	panic("not implemented")
}

func (c *corev1Impl) PodTemplates(namespace string) corev1.PodTemplateInterface {
	panic("not implemented")
}

func (c *corev1Impl) ReplicationControllers(namespace string) corev1.ReplicationControllerInterface {
	panic("not implemented")
}

func (c *corev1Impl) ResourceQuotas(namespace string) corev1.ResourceQuotaInterface {
	panic("not implemented")
}

func (c *corev1Impl) Secrets(namespace string) corev1.SecretInterface {
	panic("not implemented")
}

func (c *corev1Impl) ServiceAccounts(namespace string) corev1.ServiceAccountInterface {
	panic("not implemented")
}

func (n *nodeImpl) UpdateStatus(*apicorev1.Node) (*apicorev1.Node, error) {
	panic("not implemented")
}

func (n *nodeImpl) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}
func (n *nodeImpl) Get(name string, options metav1.GetOptions) (*apicorev1.Node, error) {
	panic("not implemented")
}

func (n *nodeImpl) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *apicorev1.Node, err error) {
	panic("not implemented")
}
func (n *nodeImpl) PatchStatus(nodeName string, data []byte) (*apicorev1.Node, error) {
	panic("not implemented")
}

func (p *podImpl) UpdateStatus(*apicorev1.Pod) (*apicorev1.Pod, error) {
	panic("not implemented")
}

func (p *podImpl) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}
func (p *podImpl) Get(name string, options metav1.GetOptions) (*apicorev1.Pod, error) {
	panic("not implemented")
}

func (p *podImpl) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *apicorev1.Pod, err error) {
	panic("not implemented")
}

func (p *podImpl) Bind(binding *apicorev1.Binding) error {
	panic("not implemented")
}

func (p *podImpl) Evict(eviction *policy.Eviction) error {
	panic("not implemented")
}

func (p *podImpl) GetLogs(name string, opts *apicorev1.PodLogOptions) *rest.Request {
	panic("not implemented")
}

func (s *serviceImpl) UpdateStatus(*apicorev1.Service) (*apicorev1.Service, error) {
	panic("not implemented")
}

func (s *serviceImpl) Get(name string, options metav1.GetOptions) (*apicorev1.Service, error) {
	panic("not implemented")
}

func (s *serviceImpl) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *apicorev1.Service, err error) {
	panic("not implemented")
}

func (s *serviceImpl) ProxyGet(scheme, name, port, path string, params map[string]string) rest.ResponseWrapper {
	panic("not implemented")
}

func (e *endpointsImpl) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	panic("not implemented")
}

func (e *endpointsImpl) Get(name string, options metav1.GetOptions) (*apicorev1.Endpoints, error) {
	panic("not implemented")
}

func (e *endpointsImpl) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *apicorev1.Endpoints, err error) {
	panic("not implemented")
}
