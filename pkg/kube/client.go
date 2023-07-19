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
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/credentials"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kubeExtClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	metadatafake "k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapibeta "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayapiclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	gatewayapifake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"istio.io/api/annotation"
	"istio.io/api/label"
	clientextensions "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	clientnetworkingalpha "istio.io/client-go/pkg/apis/networking/v1alpha3"
	clientnetworkingbeta "istio.io/client-go/pkg/apis/networking/v1beta1"
	clientsecurity "istio.io/client-go/pkg/apis/security/v1beta1"
	clienttelemetry "istio.io/client-go/pkg/apis/telemetry/v1alpha1"
	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/informerfactory"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/istio/pkg/kube/mcs"
	"istio.io/istio/pkg/lazy"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/sleep"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/version"
)

const (
	defaultLocalAddress = "localhost"
	fieldManager        = "istio-kube-client"
	RunningStatus       = "status.phase=Running"
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

	// Informers returns an informer factory
	Informers() informerfactory.InformerFactory

	// CrdWatcher returns the CRD watcher for this client
	CrdWatcher() kubetypes.CrdWatcher

	// RunAndWait starts all informers and waits for their caches to sync.
	// Warning: this must be called AFTER .Informer() is called, which will register the informer.
	RunAndWait(stop <-chan struct{})

	// WaitForCacheSync waits for all cache functions to sync, as well as all informers started by the *fake* client.
	WaitForCacheSync(name string, stop <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool

	// GetKubernetesVersion returns the Kubernetes server version
	GetKubernetesVersion() (*kubeVersion.Info, error)

	// Shutdown closes all informers and waits for them to terminate
	Shutdown()

	// ClusterID returns the cluster this client is connected to
	ClusterID() cluster.ID
}

// CLIClient is an extended client with additional helpers/functionality for Istioctl and testing.
// CLIClient is not appropriate for controllers, as it does a number of highly privileged or highly risky operations
// such as `exec`, `port-forward`, etc.
type CLIClient interface {
	Client
	// Revision of the Istio control plane.
	Revision() string

	// EnvoyDo makes a http request to the Envoy in the specified pod.
	EnvoyDo(ctx context.Context, podName, podNamespace, method, path string) ([]byte, error)

	// EnvoyDoWithPort makes a http request to the Envoy in the specified pod and port.
	EnvoyDoWithPort(ctx context.Context, podName, podNamespace, method, path string, port int) ([]byte, error)

	// AllDiscoveryDo makes a http request to each Istio discovery instance.
	AllDiscoveryDo(ctx context.Context, namespace, path string) (map[string][]byte, error)

	// GetIstioVersions gets the version for each Istio control plane component.
	GetIstioVersions(ctx context.Context, namespace string) (*version.MeshInfo, error)

	// PodsForSelector finds pods matching selector.
	PodsForSelector(ctx context.Context, namespace string, labelSelectors ...string) (*v1.PodList, error)

	// GetIstioPods retrieves the pod objects for Istio deployments
	GetIstioPods(ctx context.Context, namespace string, opts metav1.ListOptions) ([]v1.Pod, error)

	// GetProxyPods retrieves all the proxy pod objects: sidecar injected pods and gateway pods.
	GetProxyPods(ctx context.Context, limit int64, token string) (*v1.PodList, error)

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
	UtilFactory() PartialFactory

	// SetPortManager overrides the default port manager to provision local ports
	SetPortManager(PortManager)

	// InvalidateDiscovery invalidates the discovery client, useful after manually changing CRD's
	InvalidateDiscovery()
}

type PortManager func() (uint16, error)

var (
	_ Client    = &client{}
	_ CLIClient = &client{}
)

// NewFakeClient creates a new, fake, client
func NewFakeClient(objects ...runtime.Object) CLIClient {
	c := &client{
		informerWatchesPending: atomic.NewInt32(0),
		clusterID:              "fake",
	}
	c.kube = fake.NewSimpleClientset(objects...)

	c.informerFactory = informerfactory.NewSharedInformerFactory()
	s := FakeIstioScheme

	c.metadata = metadatafake.NewSimpleMetadataClient(s)
	c.dynamic = dynamicfake.NewSimpleDynamicClient(s)
	c.istio = istiofake.NewSimpleClientset()
	c.gatewayapi = gatewayapifake.NewSimpleClientset()
	c.extSet = extfake.NewSimpleClientset()

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
	// https://github.com/kubernetes/client-go/issues/439
	createReactor := func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		ret = action.(clienttesting.CreateAction).GetObject()
		meta, ok := ret.(metav1.Object)
		if !ok {
			return
		}

		if meta.GetName() == "" && meta.GetGenerateName() != "" {
			meta.SetName(names.SimpleNameGenerator.GenerateName(meta.GetGenerateName()))
		}

		return
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
		fc.PrependReactor("create", "*", createReactor)
	}

	c.fastSync = true

	c.version = lazy.NewWithRetry(c.kube.Discovery().ServerVersion)

	if NewCrdWatcher != nil {
		c.crdWatcher = NewCrdWatcher(c)
	}

	return c
}

func NewFakeClientWithVersion(minor string, objects ...runtime.Object) CLIClient {
	c := NewFakeClient(objects...).(*client)
	c.Kube().Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &kubeVersion.Info{Major: "1", Minor: minor, GitVersion: fmt.Sprintf("v1.%v.0", minor)}
	return c
}

type fakeClient interface {
	PrependReactor(verb, resource string, reaction clienttesting.ReactionFunc)
	PrependWatchReactor(resource string, reaction clienttesting.WatchReactionFunc)
	Tracker() clienttesting.ObjectTracker
}

// Client is a helper wrapper around the Kube RESTClient for istioctl -> Pilot/Envoy/Mesh related things
type client struct {
	clientFactory *clientFactory
	config        *rest.Config
	clusterID     cluster.ID

	informerFactory informerfactory.InformerFactory

	extSet     kubeExtClient.Interface
	kube       kubernetes.Interface
	dynamic    dynamic.Interface
	metadata   metadata.Interface
	istio      istioclient.Interface
	gatewayapi gatewayapiclient.Interface

	started atomic.Bool
	// If enabled, will wait for cache syncs with extremely short delay. This should be used only for tests
	fastSync               bool
	informerWatchesPending *atomic.Int32

	// These may be set only when creating an extended client.
	revision        string
	restClient      *rest.RESTClient
	discoveryClient discovery.CachedDiscoveryInterface
	mapper          meta.ResettableRESTMapper

	version lazy.Lazy[*kubeVersion.Info]

	portManager PortManager

	crdWatcher kubetypes.CrdWatcher

	// http is a client for HTTP requests
	http *http.Client
}

// newClientInternal creates a Kubernetes client from the given factory.
func newClientInternal(clientFactory *clientFactory, revision string, cluster cluster.ID) (*client, error) {
	var c client
	var err error

	c.clientFactory = clientFactory

	c.config, err = clientFactory.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	c.clusterID = cluster
	c.revision = revision

	c.restClient, err = clientFactory.RESTClient()
	if err != nil {
		return nil, err
	}

	c.discoveryClient, err = clientFactory.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	c.mapper, err = clientFactory.mapper.Get()
	if err != nil {
		return nil, err
	}

	c.informerFactory = informerfactory.NewSharedInformerFactory()

	c.kube, err = kubernetes.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}

	c.metadata, err = metadata.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}

	c.dynamic, err = dynamic.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}

	c.istio, err = istioclient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}

	c.gatewayapi, err = gatewayapiclient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}

	c.extSet, err = kubeExtClient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.portManager = defaultAvailablePort

	c.http = &http.Client{
		Timeout: time.Second * 15,
	}
	var clientWithTimeout kubernetes.Interface
	clientWithTimeout = c.kube
	restConfig := c.RESTConfig()
	if restConfig != nil {
		restConfig.Timeout = time.Second * 5
		kubeClient, err := kubernetes.NewForConfig(restConfig)
		if err == nil {
			clientWithTimeout = kubeClient
		}
	}
	c.version = lazy.NewWithRetry(clientWithTimeout.Discovery().ServerVersion)

	return &c, nil
}

// EnableCrdWatcher enables the CRD watcher on the client.
func EnableCrdWatcher(c Client) Client {
	if NewCrdWatcher == nil {
		panic("NewCrdWatcher is unset. Likely the crd watcher library is not imported anywhere")
	}
	c.(*client).crdWatcher = NewCrdWatcher(c)
	return c
}

var NewCrdWatcher func(Client) kubetypes.CrdWatcher

// NewDefaultClient returns a default client, using standard Kubernetes config resolution to determine
// the cluster to access.
func NewDefaultClient() (Client, error) {
	return NewClient(BuildClientCmd("", ""), "")
}

// NewCLIClient creates a Kubernetes client from the given ClientConfig. The "revision" parameter
// controls the behavior of GetIstioPods, by selecting a specific revision of the control plane.
// This is appropriate for use in CLI libraries because it exposes functionality unsafe for in-cluster controllers,
// and uses standard CLI (kubectl) caching.
func NewCLIClient(clientConfig clientcmd.ClientConfig, revision string) (CLIClient, error) {
	return newClientInternal(newClientFactory(clientConfig, true), revision, "")
}

// NewClient creates a Kubernetes client from the given rest config.
func NewClient(clientConfig clientcmd.ClientConfig, cluster cluster.ID) (Client, error) {
	return newClientInternal(newClientFactory(clientConfig, false), "", cluster)
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

func (c *client) Informers() informerfactory.InformerFactory {
	return c.informerFactory
}

func (c *client) CrdWatcher() kubetypes.CrdWatcher {
	return c.crdWatcher
}

// RunAndWait starts all informers and waits for their caches to sync.
// Warning: this must be called AFTER .Informer() is called, which will register the informer.
func (c *client) RunAndWait(stop <-chan struct{}) {
	c.Run(stop)
	if c.fastSync {
		if c.crdWatcher != nil {
			c.WaitForCacheSync("crd watcher", stop, c.crdWatcher.HasSynced)
		}
		// WaitForCacheSync will virtually never be synced on the first call, as its called immediately after Start()
		// This triggers a 100ms delay per call, which is often called 2-3 times in a test, delaying tests.
		// Instead, we add an aggressive sync polling
		fastWaitForCacheSync(stop, c.informerFactory)
		_ = wait.PollUntilContextTimeout(context.Background(), time.Microsecond*100, wait.ForeverTestTimeout, true, func(ctx context.Context) (bool, error) {
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
		if c.crdWatcher != nil {
			c.WaitForCacheSync("crd watcher", stop, c.crdWatcher.HasSynced)
		}
		c.informerFactory.WaitForCacheSync(stop)
	}
}

func (c *client) Shutdown() {
	c.informerFactory.Shutdown()
}

func (c *client) Run(stop <-chan struct{}) {
	c.informerFactory.Start(stop)
	if c.crdWatcher != nil {
		c.crdWatcher.Run(stop)
	}
	alreadyStarted := c.started.Swap(true)
	if alreadyStarted {
		log.Debugf("cluster %q kube client started again", c.clusterID)
	} else {
		log.Infof("cluster %q kube client started", c.clusterID)
	}
}

func (c *client) GetKubernetesVersion() (*kubeVersion.Info, error) {
	return c.version.Get()
}

func (c *client) ClusterID() cluster.ID {
	return c.clusterID
}

// Wait for cache sync immediately, rather than with 100ms delay which slows tests
// See https://github.com/kubernetes/kubernetes/issues/95262#issuecomment-703141573
func fastWaitForCacheSync(stop <-chan struct{}, informerFactory informerfactory.InformerFactory) {
	returnImmediately := make(chan struct{})
	close(returnImmediately)
	_ = wait.PollUntilContextTimeout(context.Background(), time.Microsecond*100, wait.ForeverTestTimeout, true, func(context.Context) (bool, error) {
		select {
		case <-stop:
			return false, fmt.Errorf("channel closed")
		default:
		}
		return informerFactory.WaitForCacheSync(returnImmediately), nil
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
func WaitForCacheSync(name string, stop <-chan struct{}, cacheSyncs ...cache.InformerSynced) (r bool) {
	t0 := time.Now()
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
	attempt := 0
	defer func() {
		if r {
			log.WithLabels("name", name, "attempt", attempt, "time", time.Since(t0)).Debugf("sync complete")
		} else {
			log.WithLabels("name", name, "attempt", attempt, "time", time.Since(t0)).Errorf("sync failed")
		}
	}()
	for {
		select {
		case <-stop:
			return false
		default:
		}
		attempt++
		res := f()
		if res {
			return true
		}
		delay *= 2
		if delay > max {
			delay = max
		}
		log.WithLabels("name", name, "attempt", attempt, "time", time.Since(t0)).Debugf("waiting for sync...")
		if attempt%50 == 0 {
			// Log every 50th attempt (5s) at info, to avoid too much noisy
			log.WithLabels("name", name, "attempt", attempt, "time", time.Since(t0)).Infof("waiting for sync...")
		}
		if !sleep.Until(stop, delay) {
			return false
		}
	}
}

// WaitForCacheSync is a specialized version of the general WaitForCacheSync function which also
// handles fake client syncing.
// This is only required in cases where fake clients are used without RunAndWait.
func (c *client) WaitForCacheSync(name string, stop <-chan struct{}, cacheSyncs ...cache.InformerSynced) bool {
	if c.informerWatchesPending == nil {
		return WaitForCacheSync(name, stop, cacheSyncs...)
	}
	syncFns := append(cacheSyncs, func() bool {
		return c.informerWatchesPending.Load() == 0
	})
	return WaitForCacheSync(name, stop, syncFns...)
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
	err = exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{
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
	istiods, err := c.GetIstioPods(ctx, istiodNamespace, metav1.ListOptions{
		LabelSelector: "app=istiod",
		FieldSelector: RunningStatus,
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

	fw, err := c.NewPortForwarder(podName, podNamespace, "", 0, port)
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
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, formatError(err)
	}
	defer closeQuietly(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, formatError(err)
	}

	return out, nil
}

func (c *client) GetIstioPods(ctx context.Context, namespace string, opts metav1.ListOptions) ([]v1.Pod, error) {
	if c.revision != "" {
		if opts.LabelSelector != "" {
			opts.LabelSelector += fmt.Sprintf(",%s=%s", label.IoIstioRev.Name, c.revision)
		} else {
			opts.LabelSelector = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, c.revision)
		}
	}

	pl, err := c.kube.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Pods: %v", err)
	}
	return pl.Items, nil
}

func (c *client) GetIstioVersions(ctx context.Context, namespace string) (*version.MeshInfo, error) {
	pods, err := c.GetIstioPods(ctx, namespace, metav1.ListOptions{
		LabelSelector: "app=istiod",
		FieldSelector: RunningStatus,
	})
	if err != nil {
		return nil, err
	}
	// Pod maybe running but not ready, so we need to check the container status
	readyPods := make([]v1.Pod, 0)
	for _, pod := range pods {
		if CheckPodReady(&pod) == nil {
			readyPods = append(readyPods, pod)
		}
	}
	if len(readyPods) == 0 {
		return nil, fmt.Errorf("no ready Istio pods in %q", namespace)
	}

	var errs error
	res := version.MeshInfo{}
	for _, pod := range readyPods {
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

func revisionOfPod(pod *v1.Pod) string {
	if revision, ok := pod.GetLabels()[label.IoIstioRev.Name]; ok && len(revision) > 0 {
		// For istiod or gateways.
		return revision
	}
	// For pods injected.
	statusAnno, ok := pod.GetAnnotations()[annotation.SidecarStatus.Name]
	if !ok {
		return ""
	}
	var status struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(statusAnno), &status); err != nil {
		return ""
	}
	return status.Revision
}

func (c *client) GetProxyPods(ctx context.Context, limit int64, token string) (*v1.PodList, error) {
	opts := metav1.ListOptions{
		LabelSelector: label.ServiceCanonicalName.Name,
		FieldSelector: RunningStatus,
		Limit:         limit,
		Continue:      token,
	}

	// get pods from all the namespaces.
	list, err := c.kube.CoreV1().Pods(metav1.NamespaceAll).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get the pod list: %v", err)
	}

	// If we have a istio.io/rev label for the injected pods,
	// this loop may not be needed. Instead, we can use "LabelSelector"
	// to get pods in a specific revision.
	if c.revision != "" {
		items := []v1.Pod{}
		for _, p := range list.Items {
			if revisionOfPod(&p) == c.revision {
				items = append(items, p)
			}
		}
		list.Items = items
	}

	return list, nil
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
			return c.ssapplyYAMLFile(namespace, false, f)
		})
	}
	return g.Wait()
}

func (c *client) ApplyYAMLFilesDryRun(namespace string, yamlFiles ...string) error {
	g, _ := errgroup.WithContext(context.TODO())
	for _, f := range removeEmptyFiles(yamlFiles) {
		f := f
		g.Go(func() error {
			return c.ssapplyYAMLFile(namespace, true, f)
		})
	}
	return g.Wait()
}

func (c *client) CreatePerRPCCredentials(_ context.Context, tokenNamespace, tokenServiceAccount string, audiences []string,
	expirationSeconds int64,
) (credentials.PerRPCCredentials, error) {
	return NewRPCCredentials(c, tokenNamespace, tokenServiceAccount, audiences, expirationSeconds, 60)
}

func (c *client) UtilFactory() PartialFactory {
	return c.clientFactory
}

func (c *client) ssapplyYAMLFile(namespace string, dryRun bool, file string) error {
	d, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	cfgs := yml.SplitString(string(d))
	for _, cfg := range cfgs {
		if err := c.ssapplyYAML(cfg, namespace, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) ssapplyYAML(cfg string, namespace string, dryRun bool) error {
	obj, dr, err := c.buildObject(cfg, namespace)
	if err != nil {
		if runtime.IsMissingKind(err) {
			log.Infof("skip applying, not a Kubernetes kind")
			return nil
		}
		return err
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	force := true
	_, err = dr.Patch(context.Background(), obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		DryRun:       getDryRun(dryRun),
		Force:        &force,
		FieldManager: "istio-ci",
	})
	// If we are changing CRDs, invalidate the discovery client so future calls will not fail
	if !dryRun && obj.GetKind() == gvk.CustomResourceDefinition.Kind {
		c.InvalidateDiscovery()
	}

	return err
}

func (c *client) deleteYAMLFile(namespace string, dryRun bool, file string) error {
	d, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	cfgs := yml.SplitString(string(d))
	for _, cfg := range cfgs {
		if err := c.deleteYAML(cfg, namespace, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) deleteYAML(cfg, namespace string, dryRun bool) error {
	obj, dr, err := c.buildObject(cfg, namespace)
	if err != nil {
		if runtime.IsMissingKind(err) {
			log.Infof("skip delete, not a Kubernetes kind")
			return nil
		}
		return err
	}
	err = dr.Delete(context.Background(), obj.GetName(), metav1.DeleteOptions{
		DryRun: getDryRun(dryRun),
	})
	if kerrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *client) InvalidateDiscovery() {
	c.discoveryClient.Invalidate()
	c.mapper.Reset()
}

func (c *client) DeleteYAMLFiles(namespace string, yamlFiles ...string) (err error) {
	yamlFiles = removeEmptyFiles(yamlFiles)

	// Run each delete concurrently and collect the errors.
	errs := make([]error, len(yamlFiles))
	g, _ := errgroup.WithContext(context.TODO())
	for i, f := range yamlFiles {
		i, f := i, f
		g.Go(func() error {
			errs[i] = c.deleteYAMLFile(namespace, false, f)
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
			errs[i] = c.deleteYAMLFile(namespace, true, f)
			return errs[i]
		})
	}
	_ = g.Wait()
	return multierror.Append(nil, errs...).ErrorOrNil()
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

var decUnstructured = yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

func getDryRun(dryRun bool) []string {
	var dryRunArgs []string
	if dryRun {
		dryRunArgs = []string{metav1.DryRunAll}
	}
	return dryRunArgs
}

// buildObject takes a config YAML and default namespace and returns the same object as Unstructured, along with the client to access it
func (c *client) buildObject(cfg string, namespace string) (*unstructured.Unstructured, dynamic.ResourceInterface, error) {
	obj := &unstructured.Unstructured{}
	_, gvk, err := decUnstructured.Decode([]byte(cfg), nil, obj)
	if err != nil {
		return nil, nil, err
	}

	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("mapping: %v", err)
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = namespace
		} else if namespace != "" && ns != namespace {
			return nil, nil, fmt.Errorf("object %v/%v provided namespace %q but apply called with %q", gvk, obj.GetName(), ns, namespace)
		}
		// namespaced resources should specify the namespace
		dr = c.dynamic.Resource(mapping.Resource).Namespace(ns)
	} else {
		// for cluster-wide resources
		dr = c.dynamic.Resource(mapping.Resource)
	}
	return obj, dr, nil
}

// IstioScheme returns a scheme will all known Istio-related types added
var (
	IstioScheme = istioScheme()
	IstioCodec  = serializer.NewCodecFactory(IstioScheme)
)

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
		// version is '1.12.0' || '1.12.0-custom-build'
		// revision is '016bc46f4a5e0ef3fa135b3c5380ab7765467c1a' || '016bc46f4a5e0ef3fa135b3c5380ab7765467c1a-dirty'
		// status is 'Modified' || 'Clean'
		// Ref From common/scripts/report_build_info.sh
		serverInfo.Version = strings.Join(versionParts[:nParts-2], "-")
		serverInfo.GitRevision = versionParts[nParts-2]
		serverInfo.BuildStatus = versionParts[nParts-1]
		if serverInfo.GitRevision == "dirty" {
			serverInfo.GitRevision = strings.Join([]string{versionParts[nParts-3], "dirty"}, "-")
			serverInfo.Version = strings.Join(versionParts[:nParts-3], "-")
		}
		serverInfo.GitTag = serverInfo.Version
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

func SetRevisionForTest(c CLIClient, rev string) CLIClient {
	tc := c.(*client)
	tc.revision = rev
	return tc
}
