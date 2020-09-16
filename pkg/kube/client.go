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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	v1 "k8s.io/api/core/v1"
	kubeExtClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	extfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeVersion "k8s.io/apimachinery/pkg/version"
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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	metadatafake "k8s.io/client-go/metadata/fake"
	"k8s.io/client-go/metadata/metadatainformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/cmd/apply"
	kubectlDelete "k8s.io/kubectl/pkg/cmd/delete"
	"k8s.io/kubectl/pkg/cmd/util"
	serviceapisclient "sigs.k8s.io/service-apis/pkg/client/clientset/versioned"
	serviceapisfake "sigs.k8s.io/service-apis/pkg/client/clientset/versioned/fake"
	serviceapisinformer "sigs.k8s.io/service-apis/pkg/client/informers/externalversions"

	istioclient "istio.io/client-go/pkg/clientset/versioned"
	istiofake "istio.io/client-go/pkg/clientset/versioned/fake"
	istioinformer "istio.io/client-go/pkg/informers/externalversions"

	"istio.io/api/label"

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
	// TODO - stop embedding this, it will conflict with future additions. Use Kube() instead is preferred
	// TODO - add istio/client-go and service-apis
	kubernetes.Interface
	// RESTConfig returns the Kubernetes rest.Config used to configure the clients.
	RESTConfig() *rest.Config

	// Rest returns the raw Kubernetes REST client.
	REST() rest.Interface

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

	// ServiceApis returns the service-apis kube client.
	ServiceApis() serviceapisclient.Interface

	// KubeInformer returns an informer for core kube client
	KubeInformer() informers.SharedInformerFactory

	// DynamicInformer returns an informer for dynamic client
	DynamicInformer() dynamicinformer.DynamicSharedInformerFactory

	// MetadataInformer returns an informer for metadata client
	MetadataInformer() metadatainformer.SharedInformerFactory

	// IstioInformer returns an informer for the istio client
	IstioInformer() istioinformer.SharedInformerFactory

	// ServiceApisInformer returns an informer for the service-apis client
	ServiceApisInformer() serviceapisinformer.SharedInformerFactory

	// RunAndWait starts all informers and waits for their caches to sync.
	// Warning: this must be called AFTER .Informer() is called, which will register the informer.
	RunAndWait(stop <-chan struct{})
}

// ExtendedClient is an extended client with additional helpers/functionality for Istioctl and testing.
type ExtendedClient interface {
	Client
	// Revision of the Istio control plane.
	Revision() string

	// GetKubernetesVersion returns the Kubernetes server version
	GetKubernetesVersion() (*kubeVersion.Info, error)

	// EnvoyDo makes an http request to the Envoy in the specified pod.
	EnvoyDo(ctx context.Context, podName, podNamespace, method, path string, body []byte) ([]byte, error)

	// AllDiscoveryDo makes an http request to each Istio discovery instance.
	AllDiscoveryDo(ctx context.Context, namespace, path string) (map[string][]byte, error)

	// GetIstioVersions gets the version for each Istio control plane component.
	GetIstioVersions(ctx context.Context, namespace string) (*version.MeshInfo, error)

	// PodsForSelector finds pods matching selector.
	PodsForSelector(ctx context.Context, namespace string, labelSelectors ...string) (*v1.PodList, error)

	// GetIstioPods retrieves the pod objects for Istio deployments
	GetIstioPods(ctx context.Context, namespace string, params map[string]string) ([]v1.Pod, error)

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
}

var _ Client = &client{}
var _ ExtendedClient = &client{}

const resyncInterval = 0

func NewFakeClient(objects ...runtime.Object) Client {
	var c client
	c.Interface = fake.NewSimpleClientset(objects...)
	c.kube = c.Interface
	c.kubeInformer = informers.NewSharedInformerFactory(c.Interface, resyncInterval)

	s := runtime.NewScheme()
	if err := metav1.AddMetaToScheme(s); err != nil {
		panic(err.Error())
	}

	c.metadata = metadatafake.NewSimpleMetadataClient(s)
	c.metadataInformer = metadatainformer.NewSharedInformerFactory(c.metadata, resyncInterval)

	c.dynamic = dynamicfake.NewSimpleDynamicClient(s)
	c.dynamicInformer = dynamicinformer.NewDynamicSharedInformerFactory(c.dynamic, resyncInterval)

	c.istio = istiofake.NewSimpleClientset()
	c.istioInformer = istioinformer.NewSharedInformerFactoryWithOptions(c.istio, resyncInterval)

	c.serviceapis = serviceapisfake.NewSimpleClientset()
	c.serviceapisInformers = serviceapisinformer.NewSharedInformerFactory(c.serviceapis, resyncInterval)

	c.extSet = extfake.NewSimpleClientset()

	return &c
}

// Client is a helper wrapper around the Kube RESTClient for istioctl -> Pilot/Envoy/Mesh related things
type client struct {
	kubernetes.Interface

	// These may be set only when creating an extended client. TODO: remove this entirely
	clientFactory util.Factory
	restClient    *rest.RESTClient
	revision      string

	config *rest.Config

	extSet        kubeExtClient.Interface
	versionClient discovery.ServerVersionInterface

	kube         kubernetes.Interface
	kubeInformer informers.SharedInformerFactory

	dynamic         dynamic.Interface
	dynamicInformer dynamicinformer.DynamicSharedInformerFactory

	metadata         metadata.Interface
	metadataInformer metadatainformer.SharedInformerFactory

	istio         istioclient.Interface
	istioInformer istioinformer.SharedInformerFactory

	serviceapis          serviceapisclient.Interface
	serviceapisInformers serviceapisinformer.SharedInformerFactory
}

// newClientInternal creates a Kubernetes client from the given factory.
func newClientInternal(clientFactory util.Factory, revision string) (*client, error) {
	var c client
	var err error

	c.clientFactory = clientFactory
	c.revision = revision

	c.restClient, err = clientFactory.RESTClient()
	if err != nil {
		return nil, err
	}

	c.config, err = clientFactory.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	c.Interface, err = kubernetes.NewForConfig(c.config)
	c.kube = c.Interface
	if err != nil {
		return nil, err
	}
	c.kubeInformer = informers.NewSharedInformerFactory(c.Interface, resyncInterval)

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

	c.serviceapis, err = serviceapisclient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.serviceapisInformers = serviceapisinformer.NewSharedInformerFactory(c.serviceapis, resyncInterval)

	ext, err := kubeExtClient.NewForConfig(c.config)
	if err != nil {
		return nil, err
	}
	c.extSet = ext
	c.versionClient = ext

	return &c, nil
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
	cpy := *c.config
	return &cpy
}

func (c *client) REST() rest.Interface {
	return c.restClient
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

func (c *client) ServiceApis() serviceapisclient.Interface {
	return c.serviceapis
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

func (c *client) ServiceApisInformer() serviceapisinformer.SharedInformerFactory {
	return c.serviceapisInformers
}

// RunAndWait starts all informers and waits for their caches to sync.
// Warning: this must be called AFTER .Informer() is called, which will register the informer.
func (c *client) RunAndWait(stop <-chan struct{}) {
	c.kubeInformer.Start(stop)
	c.dynamicInformer.Start(stop)
	c.metadataInformer.Start(stop)
	c.istioInformer.Start(stop)
	c.serviceapisInformers.Start(stop)
	c.kubeInformer.WaitForCacheSync(stop)
	c.dynamicInformer.WaitForCacheSync(stop)
	c.metadataInformer.WaitForCacheSync(stop)
	c.istioInformer.WaitForCacheSync(stop)
	c.serviceapisInformers.WaitForCacheSync(stop)
}

func (c *client) Revision() string {
	return c.revision
}

func (c *client) GetKubernetesVersion() (*kubeVersion.Info, error) {
	return c.versionClient.ServerVersion()
}

func (c *client) PodExec(podName, podNamespace, container string, command string) (stdout, stderr string, err error) {
	defer func() {
		if err != nil {
			if len(stderr) > 0 {
				err = fmt.Errorf("error exec'ing into %s/%s %s container: %v\n%s",
					podName, podNamespace, container, err, stderr)
			}
			err = fmt.Errorf("error exec'ing into %s/%s %s container: %v",
				podName, podNamespace, container, err)
		}
	}()

	commandFields := strings.Fields(command)
	req := c.restClient.Post().
		Resource("pods").
		Name(podName).
		Namespace(podNamespace).
		SubResource("exec").
		Param("container", container).
		VersionedParams(&v1.PodExecOptions{
			Container: container,
			Command:   commandFields,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

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

func (c *client) PodLogs(ctx context.Context, podName, podNamespace, container string, previousLog bool) (string, error) {
	opts := &v1.PodLogOptions{
		Container: container,
		Previous:  previousLog,
	}
	res, err := c.CoreV1().Pods(podNamespace).GetLogs(podName, opts).Stream(ctx)
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

// proxyGet returns a response of the pod by calling it through the proxy.
// Not a part of client-go https://github.com/kubernetes/kubernetes/issues/90768
func (c *client) proxyGet(name, namespace, path string, port int) rest.ResponseWrapper {
	pathURL, err := url.Parse(path)
	if err != nil {
		log.Errorf("failed to parse path %s: %v", path, err)
		pathURL = &url.URL{Path: path}
	}
	request := c.restClient.Get().
		Namespace(namespace).
		Resource("pods").
		SubResource("proxy").
		Name(fmt.Sprintf("%s:%d", name, port)).
		Suffix(pathURL.Path)
	for key, vals := range pathURL.Query() {
		for _, val := range vals {
			request = request.Param(key, val)
		}
	}
	return request
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
		return nil, errors.New("unable to find any Pilot instances")
	}
	result := map[string][]byte{}
	for _, istiod := range istiods {
		res, err := c.proxyGet(istiod.Name, istiod.Namespace, path, 8080).DoRaw(ctx)
		if err != nil {
			return nil, err
		}
		if len(res) > 0 {
			result[istiod.Name] = res
		}
	}
	return result, err
}

func (c *client) EnvoyDo(ctx context.Context, podName, podNamespace, method, path string, _ []byte) ([]byte, error) {
	formatError := func(err error) error {
		return fmt.Errorf("failure running port forward process: %v", err)
	}

	fw, err := c.NewPortForwarder(podName, podNamespace, "127.0.0.1", 0, 15000)
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
	out, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, formatError(err)
	}

	return out, nil
}

func (c *client) GetIstioPods(ctx context.Context, namespace string, params map[string]string) ([]v1.Pod, error) {
	if c.revision != "" {
		labelSelector, ok := params["labelSelector"]
		if ok {
			params["labelSelector"] = fmt.Sprintf("%s,%s=%s", labelSelector, label.IstioRev, c.revision)
		} else {
			params["labelSelector"] = fmt.Sprintf("%s=%s", label.IstioRev, c.revision)
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
		"labelSelector": "istio=pilot",
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
		result, err := c.proxyGet(pod.Name, pod.Namespace, "/version", 15014).DoRaw(ctx)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("error port-forewarding into %s : %v", pod.Name, err))
			continue
		}
		if len(result) > 0 {
			versionParts := strings.Split(string(result), "-")
			nParts := len(versionParts)
			if nParts >= 3 {
				server.Info.Version = strings.Join(versionParts[0:nParts-2], "-")
				server.Info.GitTag = server.Info.Version
				server.Info.GitRevision = versionParts[nParts-2]
				server.Info.BuildStatus = versionParts[nParts-1]
			} else {
				server.Info.Version = string(result)
			}
			// (Golang version not available through :15014/version endpoint)

			res = append(res, server)
		}
	}
	return &res, errs
}

func (c *client) NewPortForwarder(podName, ns, localAddress string, localPort int, podPort int) (PortForwarder, error) {
	return newPortForwarder(c.config, podName, ns, localAddress, localPort, podPort)
}

func (c *client) PodsForSelector(ctx context.Context, namespace string, labelSelectors ...string) (*v1.PodList, error) {
	return c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: strings.Join(labelSelectors, ","),
	})
}

func (c *client) ApplyYAMLFiles(namespace string, yamlFiles ...string) error {
	for _, f := range removeEmptyFiles(yamlFiles) {
		if err := c.applyYAMLFile(namespace, false, f); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) ApplyYAMLFilesDryRun(namespace string, yamlFiles ...string) error {
	for _, f := range removeEmptyFiles(yamlFiles) {
		if err := c.applyYAMLFile(namespace, true, f); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) applyYAMLFile(namespace string, dryRun bool, file string) error {
	dynamicClient, err := c.clientFactory.DynamicClient()
	if err != nil {
		return err
	}
	discoveryClient, err := c.clientFactory.ToDiscoveryClient()
	if err != nil {
		return err
	}

	// Create the options.
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()
	opts := apply.NewApplyOptions(streams)
	opts.DynamicClient = dynamicClient
	opts.DryRunVerifier = resource.NewDryRunVerifier(dynamicClient, discoveryClient)
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

	opts.DeleteFlags.FileNameFlags.Filenames = &[]string{file}
	opts.DeleteOptions = &kubectlDelete.DeleteOptions{
		DynamicClient:   dynamicClient,
		IOStreams:       streams,
		FilenameOptions: opts.DeleteFlags.FileNameFlags.ToOptions(),
	}

	opts.OpenAPISchema, _ = c.clientFactory.OpenAPISchema()

	opts.Validator, err = c.clientFactory.Validator(true)
	if err != nil {
		return err
	}
	opts.Builder = c.clientFactory.NewBuilder()
	opts.Mapper, err = c.clientFactory.ToRESTMapper()
	if err != nil {
		return err
	}

	opts.PostProcessorFn = opts.PrintAndPrunePostProcessor()

	if err := opts.Run(); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		return fmt.Errorf("%v: %s", err, s)
	}
	return nil
}

func (c *client) DeleteYAMLFiles(namespace string, yamlFiles ...string) (err error) {
	for _, f := range removeEmptyFiles(yamlFiles) {
		err = multierror.Append(err, c.deleteFile(namespace, false, f)).ErrorOrNil()
	}
	return err
}

func (c *client) DeleteYAMLFilesDryRun(namespace string, yamlFiles ...string) (err error) {
	for _, f := range removeEmptyFiles(yamlFiles) {
		err = multierror.Append(err, c.deleteFile(namespace, true, f)).ErrorOrNil()
	}
	return err
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

	dynamicClient, err := c.clientFactory.DynamicClient()
	if err != nil {
		return err
	}
	discoveryClient, err := c.clientFactory.ToDiscoveryClient()
	if err != nil {
		return err
	}
	opts := kubectlDelete.DeleteOptions{
		FilenameOptions:  fileOpts,
		Cascade:          true,
		GracePeriod:      -1,
		IgnoreNotFound:   true,
		WaitForDeletion:  true,
		WarnClusterScope: enforceNamespace,
		DynamicClient:    dynamicClient,
		DryRunVerifier:   resource.NewDryRunVerifier(dynamicClient, discoveryClient),
		IOStreams:        streams,
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

	opts.Mapper, err = c.clientFactory.ToRESTMapper()
	if err != nil {
		return err
	}

	if err := opts.RunDelete(c.clientFactory); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		return fmt.Errorf("%v: %s", err, s)
	}
	return nil
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
