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

package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/credentials"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kubeExtClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	kubeExtInformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	metadatafake "k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/cmd/apply"
	kubectlDelete "k8s.io/kubectl/pkg/cmd/delete"
	"k8s.io/kubectl/pkg/cmd/util"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapibeta "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayapiclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	gatewayapifake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
	gatewayapiinformer "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"

	"istio.io/api/label"
	clientextensions "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	clienttelemetry "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	istioinformer "istio.io/client-go/pkg/informers/externalversions"
	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/mcs"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	defaultLocalAddress = "localhost"
	fieldManager        = "istio-kube-client"
)

// Client is a helper for common Kubernetes client operations. This contains various different kubernetes
// clients using a shared config. It is expected that all of Istiod can share the same set of clients and
// informers. Sharing informers is especially important for load on the API server/Istiod itself.
type Client interface {
	// RESTConfig returns the Kubernetes rest.Config used to configure the clients.
	RESTConfig() *rest.Config

	// Ext returns the API extensions client.
	Ext() kubeExtClient.Interface

	// Kube returns the core kube client
	Kube() kubernetes.Interface

	// Dynamic client.
	Dynamic() dynamic.Interface

	// Metadata returns the Metadata kube client.
	Metadata() metadata.Interface

	// Istio returns the Istio kube client.
	Istio() istioclient.Interface

	// GatewayAPI returns the gateway-api kube client.
	GatewayAPI() gatewayapiclient.Interface

	// KubeInformer returns an informer for core kube client
	KubeInformer() informers.SharedInformerFactory

	// DynamicInformer returns an informer for dynamic client
	DynamicInformer() dynamicinformer.DynamicSharedInformerFactory

	// MetadataInformer returns an informer for metadata client
	MetadataInformer() metadatainformer.SharedInformerFactory

	// IstioInformer returns an informer for the istio client
	IstioInformer() istioinformer.SharedInformerFactory

	// GatewayAPIInformer returns an informer for the gateway-api client
	GatewayAPIInformer() gatewayapiinformer.SharedInformerFactory

	// ExtInformer returns an informer for the extension client
	ExtInformer() kubeExtInformers.SharedInformerFactory

	// RunAndWait starts all informers and waits for their caches to sync.
	// Warning: this must be called AFTER .Informer() is called, which will register the informer.
	RunAndWait(stop <-chan struct{})

	// WaitForCacheSync waits for all cache functions to sync, as well as all informers started by the *fake* client.
	WaitForCacheSync(stop <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool

	// GetKubernetesVersion returns the Kubernetes server version
	GetKubernetesVersion() (*kubeVersion.Info, error)
}

// ExtendedClient is an extended client with additional helpers/functionality for Istioctl and testing.
type ExtendedClient interface {
	Client
	// Revision of the Istio control plane.
	Revision() string

	// EnvoyDo makes an http request to the Envoy in the specified pod.
	EnvoyDo(ctx context.Context, podName, podNamespace, method, path string) ([]byte, error)

	// EnvoyDoWithPort makes an http request to the Envoy in the specified pod and port.
	EnvoyDoWithPort(ctx context.Context, podName, podNamespace, method, path string, port int) ([]byte, error)

	// AllDiscoveryDo makes an http request to each Istio discovery instance.
	AllDiscoveryDo(ctx context.Context, namespace, path string) (map[string][]byte, error)

	// GetIstioVersions gets the version for each Istio control plane component.
	GetIstioVersions(ctx context.Context, namespace string) (*version.MeshInfo, error)

	// PodsForSelector finds pods matching selector.
	PodsForSelector(ctx context.Context, namespace string, labelSelectors ...string) (*v1.PodList, error)

	// GetIstioPods retrieves the pod objects for Istio deployments
	GetIstioPods(ctx context.Context, namespace string, params map[string]string) ([]v1.Pod, error)

	// PodExecCommands takes a list of commands and the pod data to run the commands in the specified pod.
	PodExecCommands(podName, podNamespace, container string, commands []string) (stdout string, stderr string, err error)

	// PodExec takes a command and the pod data to run the command in the specified pod.
	PodExec(podName, podNamespace, container string, command string) (stdout string, stderr string, err error)

	// PodLogs retrieves the logs for the given pod.
	PodLogs(ctx context.Context, podName string, podNamespace string, container string, previousLog bool) (string, error)

	// NewPortForwarder creates a new PortForwarder configured for the given pod. If localPort=0, a port will be
	// dynamically selected. If localAddress is empty, "localhost" is used.
	NewPortForwarder(podName string, ns string, localAddress string, localPort int, podPort int) (PortForwarder, error)

	// ApplyYAMLFiles applies the resources in the given YAML files.
	ApplyYAMLFiles(namespace string, yamlFiles ...string) error

	// ApplyYAMLFilesDryRun performs a dry run for applying the resource in the given YAML files
	ApplyYAMLFilesDryRun(namespace string, yamlFiles ...string) error

	// DeleteYAMLFiles deletes the resources in the given YAML files.
	DeleteYAMLFiles(namespace string, yamlFiles ...string) error

	// DeleteYAMLFilesDryRun performs a dry run for deleting the resources in the given YAML files.
	DeleteYAMLFilesDryRun(namespace string, yamlFiles ...string) error

	// CreatePerRPCCredentials creates a gRPC bearer token provider that can create (and renew!) Istio tokens
	CreatePerRPCCredentials(ctx context.Context, tokenNamespace, tokenServiceAccount string, audiences []string,
		expirationSeconds int64) (credentials.PerRPCCredentials, error)

	// UtilFactory returns a kubectl factory
	UtilFactory() util.Factory

	// SetPortManager overrides the default port manager to provision local ports
	SetPortManager(PortManager)
}

type PortManager func() (uint16, error)

var (
	_ Client         = &client{}
	_ ExtendedClient = &client{}
)

const resyncInterval = 0

// NewFakeClient creates a new, fake, client
func NewFakeClient(objects ...runtime.Object) ExtendedClient {
	c := &client{
		informerWatchesPending: atomic.NewInt32(0),
	}
	c.kube = fake.NewSimpleClientset(objects...)
	c.kubeInformer = informers.NewSharedInformerFactory(c.kube, resyncInterval)
	s := FakeIstioScheme

	c.metadata = metadatafake.NewSimpleMetadataClient(s)
	c.metadataInformer = metadatainformer.NewSharedInformerFactory(c.metadata, resyncInterval)
	// Support some galley tests using basicmetadata
	// If you are adding something to this list, consider other options like adding to the scheme.
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "testdata.istio.io", Version: "v1alpha1", Resource: "Kind1s"}: "Kind1List",
	}
	c.dynamic = dynamicfake.NewSimpleDynamicClientWithCustomListKinds(s, gvrToListKind)
	c.dynamicInformer = dynamicinformer.NewDynamicSharedInformerFactory(c.dynamic, resyncInterval)

	c.istio = istiofake.NewSimpleClientset()
	c.istioInformer = istioinformer.NewSharedInformerFactoryWithOptions(c.istio, resyncInterval)

	c.gatewayapi = gatewayapifake.NewSimpleClientset()
	c.gatewayapiInformer = gatewayapiinformer.NewSharedInformerFactory(c.gatewayapi, resyncInterval)

	c.extSet = extfake.NewSimpleClientset()
	c.extInformer = kubeExtInformers.NewSharedInformerFactory(c.extSet, resyncInterval)

	// https://github.com/kubernetes/kubernetes/issues/95372
	// There is a race condition in the client fakes, where events that happen between the List and Watch
	// of an informer are dropped. To avoid this, we explicitly manage the list and watch, ensuring all lists
	// have an associated watch before continuing.
	// This would likely break any direct calls to List(), but for now our tests don't do that anyways. If we need
	// to in the future we will need to identify the Lists that have a corresponding Watch, possibly by looking
	// at created Informers
	// an atomic.Int is used instead of sync.WaitGroup because wg.Add and wg.Wait cannot be called concurrently
	listReactor := func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		c.informerWatchesPending.Inc()
		return false, nil, nil
	}
	watchReactor := func(tracker clienttesting.ObjectTracker) func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
		return func(action clienttesting.Action) (handled bool, ret watch.Interface, err error) {
			gvr := action.GetResource()
			ns := action.GetNamespace()
			watch, err := tracker.Watch(gvr, ns)
			if err != nil {
				return false, nil, err
			}
			c.informerWatchesPending.Dec()
			return true, watch, nil
		}
	}
	for _, fc := range []fakeClient{
		c.kube.(*fake.Clientset),
		c.istio.(*istiofake.Clientset),
		c.gatewayapi.(*gatewayapifake.Clientset),
		c.dynamic.(*dynamicfake.FakeDynamicClient),
		c.metadata.(*metadatafake.FakeMetadataClient),
	} {
		fc.PrependReactor("list", "*", listReactor)
		fc.PrependWatchReactor("*", watchReactor(fc.Tracker()))
	}

	// discoveryv1/EndpontSlices readable from discoveryv1beta1/EndpointSlices
	c.mirrorQueue = queue.NewQueue(1 * time.Second)
	mirrorResource(
		c.mirrorQueue,
		c.kubeInformer.Discovery().V1().EndpointSlices().Informer(),
		c.kube.DiscoveryV1beta1().EndpointSlices,
		endpointSliceV1toV1beta1,
	)

	c.fastSync = true

	return c
}

func NewFakeClientWithVersion(minor string, objects ...runtime.Object) ExtendedClient {
	c := NewFakeClient(objects...).(*client)
	if minor != "" && minor != "latest" {
		c.versionOnce.Do(func() {
			c.version = &kubeVersion.Info{Major: "1", Minor: minor, GitVersion: fmt.Sprintf("v1.%v.0", minor)}
		})
	}
	return c
}

type fakeClient interface {
	PrependReactor(verb, resource string, reaction clienttesting.ReactionFunc)
	PrependWatchReactor(resource string, reaction clienttesting.WatchReactionFunc)
	Tracker() clienttesting.ObjectTracker
}

// Client is a helper wrapper around the Kube RESTClient for istioctl -> Pilot/Envoy/Mesh related things
type client struct {
	clientFactory util.Factory
	config        *rest.Config

	extSet      kubeExtClient.Interface
	extInformer kubeExtInformers.SharedInformerFactory

	kube         kubernetes.Interface
	kubeInformer informers.SharedInformerFactory

	dynamic         dynamic.Interface
	dynamicInformer dynamicinformer.DynamicSharedInformerFactory

	metadata         metadata.Interface
	metadataInformer metadatainformer.SharedInformerFactory

	istio         istioclient.Interface
	istioInformer istioinformer.SharedInformerFactory

	gatewayapi         gatewayapiclient.Interface
	gatewayapiInformer gatewayapiinformer.SharedInformerFactory

	// If enable, will wait for cache syncs with extremely short delay. This should be used only for tests
	fastSync               bool
	informerWatchesPending *atomic.Int32

	mirrorQueue        queue.Instance
	mirrorQueueStarted atomic.Bool

	// These may be set only when creating an extended client.
	revision        string
	restClient      *rest.RESTClient
	discoveryClient discovery.CachedDiscoveryInterface
	mapper          meta.RESTMapper

	versionOnce sync.Once
	version     *kubeVersion.Info

	portManager PortManager
}

// newClientInternal creates a Kubernetes client from the given factory.
func newClientInternal(clientFactory util.Factory, revision string) (*client, error) {
	var c client
	var err error

	c.clientFactory = clientFactory

	c.config, err = clientFactory.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	c.revision = revision

	c.restClient, err = clientFactory.RESTClient()
	if err != nil {
		return nil, err
	}

	c.discoveryClient, err = clientFactory.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	c.mapper, err = clientFactory.ToRESTMapper()
	if err != nil {
		return nil, err
	}

	config := rest.CopyConfig(c.config)
	config.ContentType = runtime.ContentTypeProtobuf

	c.kube, err = kubernetes.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.kubeInformer = informers.NewSharedInformerFactory(c.kube, resyncInterval)

	c.metadata, err = metadata.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.metadataInformer = metadatainformer.NewSharedInformerFactory(c.metadata, resyncInterval)

	c.dynamic, err = dynamic.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.dynamicInformer = dynamicinformer.NewDynamicSharedInformerFactory(c.dynamic, resyncInterval)

	c.istio, err = istioclient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.istioInformer = istioinformer.NewSharedInformerFactory(c.istio, resyncInterval)

	c.gatewayapi, err = gatewayapiclient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.gatewayapiInformer = gatewayapiinformer.NewSharedInformerFactory(c.gatewayapi, resyncInterval)

	c.extSet, err = kubeExtClient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.extInformer = kubeExtInformers.NewSharedInformerFactory(c.extSet, resyncInterval)

	c.portManager = defaultAvailablePort

	return &c, nil
}

// NewDefaultClient returns a default client, using standard Kubernetes config resolution to determine
// the cluster to access.
func NewDefaultClient() (ExtendedClient, error) {
	return NewExtendedClient(BuildClientCmd("", ""), "")
}

// NewExtendedClient creates a Kubernetes client from the given ClientConfig. The "revision" parameter
// controls the behavior of GetIstioPods, by selecting a specific revision of the control plane.
func NewExtendedClient(clientConfig clientcmd.ClientConfig, revision string) (ExtendedClient, error) {
	return newClientInternal(newClientFactory(clientConfig), revision)
}

// NewClient creates a Kubernetes client from the given rest config.
func NewClient(clientConfig clientcmd.ClientConfig) (Client, error) {
	return newClientInternal(newClientFactory(clientConfig), "")
}

func (c *client) RESTConfig() *rest.Config {
	if c.config == nil {
		return nil
	}
	cpy := *c.config
	return &cpy
}

func (c *client) Ext() kubeExtClient.Interface {
	return c.extSet
}

func (c *client) Dynamic() dynamic.Interface {
	return c.dynamic
}

func (c *client) Kube() kubernetes.Interface {
	return c.kube
}

func (c *client) Metadata() metadata.Interface {
	return c.metadata
}

func (c *client) Istio() istioclient.Interface {
	return c.istio
}

func (c *client) GatewayAPI() gatewayapiclient.Interface {
	return c.gatewayapi
}

func (c *client) KubeInformer() informers.SharedInformerFactory {
	return c.kubeInformer
}

func (c *client) DynamicInformer() dynamicinformer.DynamicSharedInformerFactory {
	return c.dynamicInformer
}

func (c *client) MetadataInformer() metadatainformer.SharedInformerFactory {
	return c.metadataInformer
}

func (c *client) IstioInformer() istioinformer.SharedInformerFactory {
	return c.istioInformer
}

func (c *client) GatewayAPIInformer() gatewayapiinformer.SharedInformerFactory {
	return c.gatewayapiInformer
}

func (c *client) ExtInformer() kubeExtInformers.SharedInformerFactory {
	return c.extInformer
}

// RunAndWait starts all informers and waits for their caches to sync.
// Warning: this must be called AFTER .Informer() is called, which will register the informer.
func (c *client) RunAndWait(stop <-chan struct{}) {
	if c.mirrorQueue != nil && !c.mirrorQueueStarted.Load() {
		c.mirrorQueueStarted.Store(true)
		go c.mirrorQueue.Run(stop)
	}
	c.kubeInformer.Start(stop)
	c.dynamicInformer.Start(stop)
	c.metadataInformer.Start(stop)
	c.istioInformer.Start(stop)
	c.gatewayapiInformer.Start(stop)
	c.extInformer.Start(stop)
	if c.fastSync {
		// WaitForCacheSync will virtually never be synced on the first call, as its called immediately after Start()
		// This triggers a 100ms delay per call, which is often called 2-3 times in a test, delaying tests.
		// Instead, we add an aggressive sync polling
		fastWaitForCacheSync(stop, c.kubeInformer)
		fastWaitForCacheSyncDynamic(stop, c.dynamicInformer)
		fastWaitForCacheSyncDynamic(stop, c.metadataInformer)
		fastWaitForCacheSync(stop, c.istioInformer)
		fastWaitForCacheSync(stop, c.gatewayapiInformer)
		fastWaitForCacheSync(stop, c.extInformer)
		_ = wait.PollImmediate(time.Microsecond*100, wait.ForeverTestTimeout, func() (bool, error) {
			select {
			case <-stop:
				return false, fmt.Errorf("channel closed")
			default:
			}
			if c.informerWatchesPending.Load() == 0 {
				return true, nil
			}
			return false, nil
		})
	} else {
		c.kubeInformer.WaitForCacheSync(stop)
		c.dynamicInformer.WaitForCacheSync(stop)
		c.metadataInformer.WaitForCacheSync(stop)
		c.istioInformer.WaitForCacheSync(stop)
		c.gatewayapiInformer.WaitForCacheSync(stop)
		c.extInformer.WaitForCacheSync(stop)
	}
}

func (c *client) GetKubernetesVersion() (*kubeVersion.Info, error) {
	c.versionOnce.Do(func() {
		v, err := c.kube.Discovery().ServerVersion()
		if err == nil {
			c.version = v
		}
	})
	if c.version != nil {
		return c.version, nil
	}
	// Initial attempt failed, retry on each call to this function
	v, err := c.kube.Discovery().ServerVersion()
	if err != nil {
		c.version = v
	}
	return c.version, err
}

type reflectInformerSync interface {
	WaitForCacheSync(stopCh <-chan struct{}) map[reflect.Type]bool
}

type dynamicInformerSync interface {
	WaitForCacheSync(stopCh <-chan struct{}) map[schema.GroupVersionResource]bool
}

// Wait for cache sync immediately, rather than with 100ms delay which slows tests
// See https://github.com/kubernetes/kubernetes/issues/95262#issuecomment-703141573
func fastWaitForCacheSync(stop <-chan struct{}, informerFactory reflectInformerSync) {
	returnImmediately := make(chan struct{})
	close(returnImmediately)
	_ = wait.PollImmediate(time.Microsecond*100, wait.ForeverTestTimeout, func() (bool, error) {
		select {
		case <-stop:
			return false, fmt.Errorf("channel closed")
		default:
		}
		for _, synced := range informerFactory.WaitForCacheSync(returnImmediately) {
			if !synced {
				return false, nil
			}
		}
		return true, nil
	})
}

// WaitForCacheSync waits until all caches are synced. This will return true only if things synced
// successfully before the stop channel is closed. This function also lives in the Kubernetes cache
// library. However, that library will poll with 100ms fixed interval. Often the cache syncs in a few
// ms, but we are delayed a full 100ms. This is especially apparent in tests, which previously spent
// most of their time just in the 100ms wait interval.
//
// To optimize this, this function performs exponential backoff. This is generally safe because
// cache.InformerSynced functions are ~always quick to run. However, if the sync functions do perform
// expensive checks this function may not be suitable.
func WaitForCacheSync(stop <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool {
	max := time.Millisecond * 100
	delay := time.Millisecond
	f := func() bool {
		for _, syncFunc := range cacheSyncs {
			if !syncFunc() {
				return false
			}
		}
		return true
	}
	for {
		select {
		case <-stop:
			return false
		default:
		}
		res := f()
		if res {
			return true
		}
		delay *= 2
		if delay > max {
			delay = max
		}
		select {
		case <-stop:
			return false
		case <-time.After(delay):
		}
	}
}

// WaitForCacheSync is a specialized version of the general WaitForCacheSync function which also
// handles fake client syncing.
// This is only required in cases where fake clients are used without RunAndWait.
func (c *client) WaitForCacheSync(stop <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool {
	if c.informerWatchesPending == nil {
		return WaitForCacheSync(stop, cacheSyncs...)
	}
	syncFns := append(cacheSyncs, func() bool {
		return c.informerWatchesPending.Load() == 0
	})
	return WaitForCacheSync(stop, syncFns...)
}

func fastWaitForCacheSyncDynamic(stop <-chan struct{}, informerFactory dynamicInformerSync) {
	returnImmediately := make(chan struct{})
	close(returnImmediately)
	_ = wait.PollImmediate(time.Microsecond*100, wait.ForeverTestTimeout, func() (bool, error) {
		select {
		case <-stop:
			return false, fmt.Errorf("channel closed")
		default:
		}
		for _, synced := range informerFactory.WaitForCacheSync(returnImmediately) {
			if !synced {
				return false, nil
			}
		}
		return true, nil
	})
}

func (c *client) Revision() string {
	return c.revision
}

func (c *client) PodExecCommands(podName, podNamespace, container string, commands []string) (stdout, stderr string, err error) {
	defer func() {
		if err != nil {
			if len(stderr) > 0 {
				err = fmt.Errorf("error exec'ing into %s/%s %s container: %v\n%s",
					podNamespace, podName, container, err, stderr)
			} else {
				err = fmt.Errorf("error exec'ing into %s/%s %s container: %v",
					podNamespace, podName, container, err)
			}
		}
	}()

	req := c.restClient.Post().
		Resource("pods").
		Name(podName).
		Namespace(podNamespace).
		SubResource("exec").
		Param("container", container).
		VersionedParams(&v1.PodExecOptions{
			Container: container,
			Command:   commands,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, kubescheme.ParameterCodec)

	wrapper, upgrader, err := roundTripperFor(c.config)
	if err != nil {
		return "", "", err
	}
	exec, err := remotecommand.NewSPDYExecutorForTransports(wrapper, upgrader, "POST", req.URL())
	if err != nil {
		return "", "", err
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
		Tty:    false,
	})

	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()
	return
}

func (c *client) PodExec(podName, podNamespace, container string, command string) (stdout, stderr string, err error) {
	commandFields := strings.Fields(command)
	return c.PodExecCommands(podName, podNamespace, container, commandFields)
}

func (c *client) PodLogs(ctx context.Context, podName, podNamespace, container string, previousLog bool) (string, error) {
	opts := &v1.PodLogOptions{
		Container: container,
		Previous:  previousLog,
	}
	res, err := c.kube.CoreV1().Pods(podNamespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer closeQuietly(res)

	builder := &strings.Builder{}
	if _, err = io.Copy(builder, res); err != nil {
		return "", err
	}

	return builder.String(), nil
}

func (c *client) AllDiscoveryDo(ctx context.Context, istiodNamespace, path string) (map[string][]byte, error) {
	istiods, err := c.GetIstioPods(ctx, istiodNamespace, map[string]string{
		"labelSelector": "app=istiod",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(istiods) == 0 {
		return nil, errors.New("unable to find any Istiod instances")
	}

	result := map[string][]byte{}
	for _, istiod := range istiods {
		res, err := c.portForwardRequest(ctx, istiod.Name, istiod.Namespace, http.MethodGet, path, 15014)
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			result[istiod.Name] = res
		}
	}
	// If any Discovery servers responded, treat as a success
	if len(result) > 0 {
		return result, nil
	}
	return nil, nil
}

func (c *client) EnvoyDo(ctx context.Context, podName, podNamespace, method, path string) ([]byte, error) {
	return c.portForwardRequest(ctx, podName, podNamespace, method, path, 15000)
}

func (c *client) EnvoyDoWithPort(ctx context.Context, podName, podNamespace, method, path string, port int) ([]byte, error) {
	return c.portForwardRequest(ctx, podName, podNamespace, method, path, port)
}

func (c *client) portForwardRequest(ctx context.Context, podName, podNamespace, method, path string, port int) ([]byte, error) {
	formatError := func(err error) error {
		return fmt.Errorf("failure running port forward process: %v", err)
	}

	fw, err := c.NewPortForwarder(podName, podNamespace, "127.0.0.1", 0, port)
	if err != nil {
		return nil, err
	}
	if err = fw.Start(); err != nil {
		return nil, formatError(err)
	}
	defer fw.Close()
	req, err := http.NewRequest(method, fmt.Sprintf("http://%s/%s", fw.Address(), path), nil)
	if err != nil {
		return nil, formatError(err)
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, formatError(err)
	}
	defer closeQuietly(resp.Body)
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, formatError(err)
	}

	return out, nil
}

func (c *client) GetIstioPods(ctx context.Context, namespace string, params map[string]string) ([]v1.Pod, error) {
	if c.revision != "" {
		labelSelector, ok := params["labelSelector"]
		if ok {
			params["labelSelector"] = fmt.Sprintf("%s,%s=%s", labelSelector, label.IoIstioRev.Name, c.revision)
		} else {
			params["labelSelector"] = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, c.revision)
		}
	}

	req := c.restClient.Get().
		Resource("pods").
		Namespace(namespace)
	for k, v := range params {
		req.Param(k, v)
	}

	res := req.Do(ctx)
	if res.Error() != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", res.Error())
	}
	list := &v1.PodList{}
	if err := res.Into(list); err != nil {
		return nil, fmt.Errorf("unable to parse PodList: %v", res.Error())
	}
	return list.Items, nil
}

func (c *client) GetIstioVersions(ctx context.Context, namespace string) (*version.MeshInfo, error) {
	pods, err := c.GetIstioPods(ctx, namespace, map[string]string{
		"labelSelector": "app=istiod",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no running Istio pods in %q", namespace)
	}

	var errs error
	res := version.MeshInfo{}
	for _, pod := range pods {
		component := pod.Labels["istio"]
		server := version.ServerInfo{Component: component}

		// :15014/version returns something like
		// 1.7-alpha.9c900ba74d10a1affe7c23557ef0eebd6103b03c-9c900ba74d10a1affe7c23557ef0eebd6103b03c-Clean
		result, err := c.kube.CoreV1().Pods(pod.Namespace).ProxyGet("", pod.Name, "15014", "/version", nil).DoRaw(ctx)
		if err != nil {
			bi, execErr := c.getIstioVersionUsingExec(&pod)
			if execErr != nil {
				errs = multierror.Append(errs,
					fmt.Errorf("error port-forwarding into %s.%s: %v", pod.Namespace, pod.Name, err),
					execErr,
				)
				continue
			}
			server.Info = *bi
			res = append(res, server)
			continue
		}
		if len(result) > 0 {
			setServerInfoWithIstiodVersionInfo(&server.Info, string(result))
			// (Golang version not available through :15014/version endpoint)

			res = append(res, server)
		}
	}
	return &res, errs
}

func (c *client) getIstioVersionUsingExec(pod *v1.Pod) (*version.BuildInfo, error) {
	// exclude data plane components from control plane list
	labelToPodDetail := map[string]struct {
		binary    string
		container string
	}{
		"pilot":            {"/usr/local/bin/pilot-discovery", "discovery"},
		"istiod":           {"/usr/local/bin/pilot-discovery", "discovery"},
		"citadel":          {"/usr/local/bin/istio_ca", "citadel"},
		"galley":           {"/usr/local/bin/galley", "galley"},
		"telemetry":        {"/usr/local/bin/mixs", "mixer"},
		"policy":           {"/usr/local/bin/mixs", "mixer"},
		"sidecar-injector": {"/usr/local/bin/sidecar-injector", "sidecar-injector-webhook"},
	}

	component := pod.Labels["istio"]

	// Special cases
	switch component {
	case "statsd-prom-bridge":
		// statsd-prom-bridge doesn't support version
		return nil, fmt.Errorf("statsd-prom-bridge doesn't support version")
	case "mixer":
		component = pod.Labels["istio-mixer-type"]
	}

	detail, ok := labelToPodDetail[component]
	if !ok {
		return nil, fmt.Errorf("unknown Istio component %q", component)
	}

	stdout, stderr, err := c.PodExec(pod.Name, pod.Namespace, detail.container,
		fmt.Sprintf("%s version -o json", detail.binary))
	if err != nil {
		return nil, fmt.Errorf("error exec'ing into %s %s container: %w", pod.Name, detail.container, err)
	}

	var v version.Version
	err = json.Unmarshal([]byte(stdout), &v)
	if err == nil && v.ClientVersion.Version != "" {
		return v.ClientVersion, nil
	}

	return nil, fmt.Errorf("error reading %s %s container version: %v", pod.Name, detail.container, stderr)
}

func (c *client) NewPortForwarder(podName, ns, localAddress string, localPort int, podPort int) (PortForwarder, error) {
	return newPortForwarder(c, podName, ns, localAddress, localPort, podPort)
}

func (c *client) PodsForSelector(ctx context.Context, namespace string, labelSelectors ...string) (*v1.PodList, error) {
	return c.kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: strings.Join(labelSelectors, ","),
	})
}

func (c *client) ApplyYAMLFiles(namespace string, yamlFiles ...string) error {
	g, _ := errgroup.WithContext(context.TODO())
	for _, f := range removeEmptyFiles(yamlFiles) {
		f := f
		g.Go(func() error {
			return c.applyYAMLFile(namespace, false, f)
		})
	}
	return g.Wait()
}

func (c *client) ApplyYAMLFilesDryRun(namespace string, yamlFiles ...string) error {
	g, _ := errgroup.WithContext(context.TODO())
	for _, f := range removeEmptyFiles(yamlFiles) {
		f := f
		g.Go(func() error {
			return c.applyYAMLFile(namespace, true, f)
		})
	}
	return g.Wait()
}

func (c *client) CreatePerRPCCredentials(_ context.Context, tokenNamespace, tokenServiceAccount string, audiences []string,
	expirationSeconds int64,
) (credentials.PerRPCCredentials, error) {
	return NewRPCCredentials(c, tokenNamespace, tokenServiceAccount, audiences, expirationSeconds, 60)
}

func (c *client) UtilFactory() util.Factory {
	return c.clientFactory
}

// TODO once we drop Kubernetes 1.15 support we can drop all of this code in favor of Server Side Apply
// Following https://ymmt2005.hatenablog.com/entry/2020/04/14/An_example_of_using_dynamic_client_of_k8s.io/client-go
func (c *client) applyYAMLFile(namespace string, dryRun bool, file string) error {
	// Create the options.
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()
	flags := apply.NewApplyFlags(c.clientFactory, streams)
	flags.DeleteFlags.FileNameFlags.Filenames = &[]string{file}

	cmd := apply.NewCmdApply("", c.clientFactory, streams)
	opts, err := flags.ToOptions(cmd, "", nil)
	if err != nil {
		return err
	}
	opts.DynamicClient = c.dynamic
	opts.DryRunVerifier = resource.NewQueryParamVerifier(c.dynamic, c.discoveryClient, resource.QueryParamDryRun)
	opts.FieldValidationVerifier = resource.NewQueryParamVerifier(c.dynamic, c.discoveryClient, resource.QueryParamFieldValidation)
	opts.FieldManager = fieldManager
	if dryRun {
		opts.DryRunStrategy = util.DryRunServer
	}

	// allow for a success message operation to be specified at print time
	opts.ToPrinter = func(operation string) (printers.ResourcePrinter, error) {
		opts.PrintFlags.NamePrintFlags.Operation = operation
		util.PrintFlagsWithDryRunStrategy(opts.PrintFlags, opts.DryRunStrategy)
		return opts.PrintFlags.ToPrinter()
	}

	if len(namespace) > 0 {
		opts.Namespace = namespace
		opts.EnforceNamespace = true
	} else {
		var err error
		opts.Namespace, opts.EnforceNamespace, err = c.clientFactory.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}
	}

	opts.DeleteOptions = &kubectlDelete.DeleteOptions{
		DynamicClient:   c.dynamic,
		IOStreams:       streams,
		FilenameOptions: flags.DeleteFlags.FileNameFlags.ToOptions(),
	}

	opts.OpenAPISchema, _ = c.clientFactory.OpenAPISchema()

	opts.Validator, err = c.clientFactory.Validator(metav1.FieldValidationStrict, opts.FieldValidationVerifier)
	if err != nil {
		return err
	}
	opts.Builder = c.clientFactory.NewBuilder()
	opts.Mapper = c.mapper

	opts.PostProcessorFn = opts.PrintAndPrunePostProcessor()

	if err := opts.Run(); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		return fmt.Errorf("%v: %s", err, s)
	}
	// If we are changing CRDs, invalidate the discovery client so future calls will not fail
	if !dryRun {
		f, err := os.ReadFile(file)
		if err != nil {
			log.Warnf("Failed to read %s: %v", file, err)
		}
		if len(yml.SplitYamlByKind(string(f))[gvk.CustomResourceDefinition.Kind]) > 0 {
			c.discoveryClient.Invalidate()
		}
	}
	return nil
}

func (c *client) DeleteYAMLFiles(namespace string, yamlFiles ...string) (err error) {
	yamlFiles = removeEmptyFiles(yamlFiles)

	// Run each delete concurrently and collect the errors.
	errs := make([]error, len(yamlFiles))
	g, _ := errgroup.WithContext(context.TODO())
	for i, f := range yamlFiles {
		i, f := i, f
		g.Go(func() error {
			errs[i] = c.deleteFile(namespace, false, f)
			return errs[i]
		})
	}
	_ = g.Wait()
	return multierror.Append(nil, errs...).ErrorOrNil()
}

func (c *client) DeleteYAMLFilesDryRun(namespace string, yamlFiles ...string) (err error) {
	yamlFiles = removeEmptyFiles(yamlFiles)

	// Run each delete concurrently and collect the errors.
	errs := make([]error, len(yamlFiles))
	g, _ := errgroup.WithContext(context.TODO())
	for i, f := range yamlFiles {
		i, f := i, f
		g.Go(func() error {
			errs[i] = c.deleteFile(namespace, true, f)
			return errs[i]
		})
	}
	_ = g.Wait()
	return multierror.Append(nil, errs...).ErrorOrNil()
}

func (c *client) deleteFile(namespace string, dryRun bool, file string) error {
	// Create the options.
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()

	cmdNamespace, enforceNamespace, err := c.clientFactory.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	if len(namespace) > 0 {
		cmdNamespace = namespace
		enforceNamespace = true
	}

	fileOpts := resource.FilenameOptions{
		Filenames: []string{file},
	}

	opts := kubectlDelete.DeleteOptions{
		FilenameOptions:   fileOpts,
		CascadingStrategy: metav1.DeletePropagationBackground,
		GracePeriod:       -1,
		IgnoreNotFound:    true,
		WaitForDeletion:   true,
		WarnClusterScope:  enforceNamespace,
		DynamicClient:     c.dynamic,
		DryRunVerifier:    resource.NewQueryParamVerifier(c.dynamic, c.discoveryClient, resource.QueryParamDryRun),

		IOStreams: streams,
	}
	if dryRun {
		opts.DryRunStrategy = util.DryRunServer
	}

	r := c.clientFactory.NewBuilder().
		Unstructured().
		ContinueOnError().
		NamespaceParam(cmdNamespace).DefaultNamespace().
		FilenameParam(enforceNamespace, &fileOpts).
		LabelSelectorParam(opts.LabelSelector).
		FieldSelectorParam(opts.FieldSelector).
		SelectAllParam(opts.DeleteAll).
		AllNamespaces(opts.DeleteAllNamespaces).
		Flatten().
		Do()
	err = r.Err()
	if err != nil {
		return err
	}
	opts.Result = r

	opts.Mapper = c.mapper

	if err := opts.RunDelete(c.clientFactory); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		return fmt.Errorf("%v: %s", err, s)
	}
	return nil
}

func (c *client) SetPortManager(manager PortManager) {
	c.portManager = manager
}

func closeQuietly(c io.Closer) {
	_ = c.Close()
}

func removeEmptyFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if !isEmptyFile(f) {
			out = append(out, f)
		}
	}
	return out
}

func isEmptyFile(f string) bool {
	fileInfo, err := os.Stat(f)
	if err != nil {
		return true
	}
	if fileInfo.Size() == 0 {
		return true
	}
	return false
}

// IstioScheme returns a scheme will all known Istio-related types added
var IstioScheme = istioScheme()

// FakeIstioScheme is an IstioScheme that has List type registered.
var FakeIstioScheme = func() *runtime.Scheme {
	s := istioScheme()
	// Workaround https://github.com/kubernetes/kubernetes/issues/107823
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "fake-metadata-client-group", Version: "v1", Kind: "List"}, &metav1.List{})
	return s
}()

func istioScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(kubescheme.AddToScheme(scheme))
	utilruntime.Must(mcs.AddToScheme(scheme))
	utilruntime.Must(clientnetworkingalpha.AddToScheme(scheme))
	utilruntime.Must(clientnetworkingbeta.AddToScheme(scheme))
	utilruntime.Must(clientsecurity.AddToScheme(scheme))
	utilruntime.Must(clienttelemetry.AddToScheme(scheme))
	utilruntime.Must(clientextensions.AddToScheme(scheme))
	utilruntime.Must(gatewayapi.AddToScheme(scheme))
	utilruntime.Must(gatewayapibeta.AddToScheme(scheme))
	utilruntime.Must(apis.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	return scheme
}

func setServerInfoWithIstiodVersionInfo(serverInfo *version.BuildInfo, istioInfo string) {
	versionParts := strings.Split(istioInfo, "-")
	nParts := len(versionParts)
	if nParts >= 3 {
		// The format will be like 1.12.0-016bc46f4a5e0ef3fa135b3c5380ab7765467c1a-dirty-Modified
		// version is '1.12.0'
		// revision is '016bc46f4a5e0ef3fa135b3c5380ab7765467c1a-dirty'
		// status is 'Modified'
		serverInfo.Version = versionParts[0]
		serverInfo.GitTag = serverInfo.Version
		serverInfo.GitRevision = strings.Join(versionParts[1:nParts-1], "-")
		serverInfo.BuildStatus = versionParts[nParts-1]
	} else {
		serverInfo.Version = istioInfo
	}
}

func defaultAvailablePort() (uint16, error) {
	addr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort("127.0.0.1", "0"))
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return uint16(port), l.Close()
}
