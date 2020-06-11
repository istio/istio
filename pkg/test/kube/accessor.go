//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	kubeExtClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Needed for auth
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/delete"
	"k8s.io/kubectl/pkg/cmd/util"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
)

const (
	workDirPrefix = "istio-kube-accessor-"
)

var (
	defaultRetryTimeout = retry.Timeout(time.Minute * 10)
	defaultRetryDelay   = retry.Delay(time.Second * 1)
)

// Accessor is a helper for accessing Kubernetes programmatically. It bundles some of the high-level
// operations that is frequently used by the test framework.
type Accessor interface {
	kubernetes.Interface

	// RESTConfig returns the Kubernetes rest config used to configure the clients.
	RESTConfig() *rest.Config

	// Ext returns the API extensions client.
	Ext() kubeExtClient.Interface

	// NewPortForwarder creates a new port forwarder.
	NewPortForwarder(pod kubeApiCore.Pod, localPort, remotePort uint16) (PortForwarder, error)

	// GetPods returns pods in the given namespace, based on the selectors. If no selectors are given, then
	// all pods are returned.
	GetPods(namespace string, selectors ...string) ([]kubeApiCore.Pod, error)

	// GetEvents returns events in the given namespace, based on the involvedObject.
	GetEvents(namespace string, involvedObject string) ([]kubeApiCore.Event, error)

	// WaitUntilPodsAreReady waits until the pod with the name/namespace is in ready state.
	WaitUntilPodsAreReady(fetchFunc PodFetchFunc, opts ...retry.Option) ([]kubeApiCore.Pod, error)

	// CheckPodsAreReady checks whether the pods that are selected by the given function is in ready state or not.
	CheckPodsAreReady(fetchFunc PodFetchFunc) ([]kubeApiCore.Pod, error)

	// WaitUntilServiceEndpointsAreReady will wait until the service with the given name/namespace is present, and have at least
	// one usable endpoint.
	WaitUntilServiceEndpointsAreReady(ns string, name string, opts ...retry.Option) (*kubeApiCore.Service, *kubeApiCore.Endpoints, error)

	// WaitForSecretToExist waits for the given secret up to the given waitTime.
	WaitForSecretToExist(namespace, name string, waitTime time.Duration) (*kubeApiCore.Secret, error)

	// WaitForSecretToExistOrFail calls WaitForSecretToExist and fails the given test.Failer if an error occurs.
	WaitForSecretToExistOrFail(t test.Failer, namespace, name string, waitTime time.Duration) *kubeApiCore.Secret

	// GetKubernetesVersion returns the Kubernetes server version
	GetKubernetesVersion() (*version.Info, error)

	// CreateNamespaceWithLabels with the specified name, sidecar-injection behavior, and labels
	CreateNamespaceWithLabels(ns string, istioTestingAnnotation string, labels map[string]string) error

	// NamespaceExists returns true if the given namespace exists.
	NamespaceExists(ns string) bool

	// DeleteNamespace with the given name
	DeleteNamespace(ns string) error

	// WaitForNamespaceDeletion waits until a namespace is deleted.
	WaitForNamespaceDeletion(ns string, opts ...retry.Option) error

	// GetUnstructured returns an unstructured k8s resource object based on the provided schema, namespace, and name.
	GetUnstructured(gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)

	// DeleteUnstructured deletes an unstructured k8s resource object based on the provided schema, namespace, and name.
	DeleteUnstructured(gvr schema.GroupVersionResource, namespace, name string) error

	// ApplyContents applies the given config contents using kubectl.
	ApplyContents(namespace string, contents string) ([]string, error)

	// ApplyContentsDryRun applies the given config contents using kubectl with DryRun mode.
	ApplyContentsDryRun(namespace string, contents string) ([]string, error)

	// Apply applies the config in the given filename using kubectl.
	Apply(namespace string, filename string) error

	// ApplyDryRun applies the config in the given filename using kubectl with DryRun mode.
	ApplyDryRun(namespace string, filename string) error

	// DeleteContents deletes the given config contents using kubectl.
	DeleteContents(namespace string, contents string) error

	// Delete the config in the given filename using kubectl.
	Delete(namespace string, filename string) error

	// Logs calls the logs command for the specified pod, with -c, if container is specified.
	Logs(namespace string, pod string, container string, previousLog bool) (string, error)

	// Exec executes the provided command on the specified pod/container.
	Exec(namespace, podName, containerName, command string) (string, error)
}

var _ Accessor = &accessorImpl{}

type accessorImpl struct {
	kubernetes.Interface

	clientFactory util.Factory
	baseDir       string
	workDir       string
	workDirMutex  sync.Mutex
	restConfig    *rest.Config
	extSet        *kubeExtClient.Clientset
	dynamicClient dynamic.Interface
}

// NewAccessor returns a new Accessor from a kube config file.
func NewAccessor(kubeConfig string, baseWorkDir string) (Accessor, error) {
	clientFactory := istioKube.NewClientFactory(istioKube.BuildClientCmd(kubeConfig, ""), "")
	return NewAccessorForClientFactory(clientFactory, baseWorkDir)
}

// NewAccessorForClientFactory creates a new Accessor from a ClientConfig.
func NewAccessorForClientFactory(clientFactory util.Factory, baseWorkDir string) (Accessor, error) {
	clientSet, err := clientFactory.KubernetesClientSet()
	if err != nil {
		return nil, err
	}

	restConfig, err := clientFactory.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	extSet, err := kubeExtClient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := clientFactory.DynamicClient()
	if err != nil {
		return nil, err
	}

	return &accessorImpl{
		Interface:     clientSet,
		clientFactory: clientFactory,
		restConfig:    restConfig,
		extSet:        extSet,
		dynamicClient: dynamicClient,
		baseDir:       baseWorkDir,
	}, nil
}

// RESTConfig returns the Kubernetes rest config used to configure the clients.
func (a *accessorImpl) RESTConfig() *rest.Config {
	return a.restConfig
}

// Ext returns the API extensions client.
func (a *accessorImpl) Ext() kubeExtClient.Interface {
	return a.extSet
}

// NewPortForwarder creates a new port forwarder for the given pod.
func (a *accessorImpl) NewPortForwarder(pod kubeApiCore.Pod, localPort, remotePort uint16) (PortForwarder, error) {
	return NewPortForwarder(a.restConfig, pod, localPort, remotePort)
}

// GetPods returns pods in the given namespace, based on the selectors. If no selectors are given, then
// all pods are returned.
func (a *accessorImpl) GetPods(namespace string, selectors ...string) ([]kubeApiCore.Pod, error) {
	s := strings.Join(selectors, ",")
	list, err := a.CoreV1().Pods(namespace).List(context.TODO(), kubeApiMeta.ListOptions{LabelSelector: s})

	if err != nil {
		return []kubeApiCore.Pod{}, err
	}

	return list.Items, nil
}

// GetEvents returns events in the given namespace, based on the involvedObject.
func (a *accessorImpl) GetEvents(namespace string, involvedObject string) ([]kubeApiCore.Event, error) {
	s := "involvedObject.name=" + involvedObject
	list, err := a.CoreV1().Events(namespace).List(context.TODO(), kubeApiMeta.ListOptions{FieldSelector: s})

	if err != nil {
		return []kubeApiCore.Event{}, err
	}

	return list.Items, nil
}

// WaitUntilPodsAreReady waits until the pod with the name/namespace is in ready state.
func (a *accessorImpl) WaitUntilPodsAreReady(fetchFunc PodFetchFunc, opts ...retry.Option) ([]kubeApiCore.Pod, error) {
	var pods []kubeApiCore.Pod
	_, err := retry.Do(func() (interface{}, bool, error) {

		scopes.Framework.Infof("Checking pods ready...")

		fetched, err := a.CheckPodsAreReady(fetchFunc)
		if err != nil {
			return nil, false, err
		}
		pods = fetched
		return nil, true, nil
	}, newRetryOptions(opts...)...)

	return pods, err
}

// CheckPodsAreReady checks whether the pods that are selected by the given function is in ready state or not.
func (a *accessorImpl) CheckPodsAreReady(fetchFunc PodFetchFunc) ([]kubeApiCore.Pod, error) {
	scopes.Framework.Infof("Checking pods ready...")

	fetched, err := fetchFunc()
	if err != nil {
		scopes.Framework.Infof("Failed retrieving pods: %v", err)
		return nil, err
	}

	for i, p := range fetched {
		msg := "Ready"
		if e := CheckPodReady(&p); e != nil {
			msg = e.Error()
			err = multierror.Append(err, fmt.Errorf("%s/%s: %s", p.Namespace, p.Name, msg))
		}
		scopes.Framework.Infof("  [%2d] %45s %15s (%v)", i, p.Name, p.Status.Phase, msg)
	}

	if err != nil {
		return nil, err
	}

	return fetched, nil
}

// WaitUntilServiceEndpointsAreReady will wait until the service with the given name/namespace is present, and have at least
// one usable endpoint.
func (a *accessorImpl) WaitUntilServiceEndpointsAreReady(ns string, name string,
	opts ...retry.Option) (*kubeApiCore.Service, *kubeApiCore.Endpoints, error) {
	var service *kubeApiCore.Service
	var endpoints *kubeApiCore.Endpoints
	err := retry.UntilSuccess(func() error {
		s, err := a.CoreV1().Services(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}

		eps, err := a.CoreV1().Endpoints(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}
		if len(eps.Subsets) == 0 {
			return fmt.Errorf("%s/%v endpoint not ready: no subsets", ns, name)
		}

		for _, subset := range eps.Subsets {
			if len(subset.Addresses) > 0 && len(subset.NotReadyAddresses) == 0 {
				service = s
				endpoints = eps
				return nil
			}
		}
		return fmt.Errorf("%s/%v endpoint not ready: no ready addresses", ns, name)
	}, newRetryOptions(opts...)...)

	if err != nil {
		return nil, nil, err
	}

	return service, endpoints, nil
}

// WaitForSecretToExist waits for the given secret up to the given waitTime.
func (a *accessorImpl) WaitForSecretToExist(namespace, name string, waitTime time.Duration) (*kubeApiCore.Secret, error) {
	secret := a.CoreV1().Secrets(namespace)

	watch, err := secret.Watch(context.TODO(), kubeApiMeta.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to set up watch for secret (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			secret := event.Object.(*kubeApiCore.Secret)
			if secret.GetName() == name {
				return secret, nil
			}
		case <-time.After(waitTime - time.Since(startTime)):
			return nil, fmt.Errorf("secret %v did not become existent within %v",
				name, waitTime)
		}
	}
}

// WaitForSecretToExistOrFail calls WaitForSecretToExist and fails the given test.Failer if an error occurs.
func (a *accessorImpl) WaitForSecretToExistOrFail(t test.Failer, namespace, name string,
	waitTime time.Duration) *kubeApiCore.Secret {
	t.Helper()
	s, err := a.WaitForSecretToExist(namespace, name, waitTime)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// GetKubernetesVersion returns the Kubernetes server version
func (a *accessorImpl) GetKubernetesVersion() (*version.Info, error) {
	return a.extSet.ServerVersion()
}

// CreateNamespaceWithLabels with the specified name, sidecar-injection behavior, and labels
func (a *accessorImpl) CreateNamespaceWithLabels(ns string, istioTestingAnnotation string, labels map[string]string) error {
	scopes.Framework.Debugf("Creating namespace %s ns with labels %v", ns, labels)

	n := a.newNamespaceWithLabels(ns, istioTestingAnnotation, labels)
	_, err := a.CoreV1().Namespaces().Create(context.TODO(), &n, kubeApiMeta.CreateOptions{})
	return err
}

func (a *accessorImpl) newNamespaceWithLabels(ns string, istioTestingAnnotation string, labels map[string]string) kubeApiCore.Namespace {
	if labels == nil {
		labels = make(map[string]string)
	}
	n := kubeApiCore.Namespace{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:   ns,
			Labels: labels,
		},
	}
	if istioTestingAnnotation != "" {
		n.ObjectMeta.Labels["istio-testing"] = istioTestingAnnotation
	}
	return n
}

// NamespaceExists returns true if the given namespace exists.
func (a *accessorImpl) NamespaceExists(ns string) bool {
	allNs, err := a.CoreV1().Namespaces().List(context.TODO(), kubeApiMeta.ListOptions{})
	if err != nil {
		return false
	}
	for _, n := range allNs.Items {
		if n.Name == ns {
			return true
		}
	}
	return false
}

// DeleteNamespace with the given name
func (a *accessorImpl) DeleteNamespace(ns string) error {
	scopes.Framework.Debugf("Deleting namespace: %s", ns)
	return a.CoreV1().Namespaces().Delete(context.TODO(), ns, *deleteOptionsForeground())
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func (a *accessorImpl) WaitForNamespaceDeletion(ns string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {
		_, err2 := a.CoreV1().Namespaces().Get(context.TODO(), ns, kubeApiMeta.GetOptions{})
		if err2 == nil {
			return nil, false, nil
		}

		if errors.IsNotFound(err2) {
			return nil, true, nil
		}

		return nil, false, err2
	}, newRetryOptions(opts...)...)

	return err
}

// GetUnstructured returns an unstructured k8s resource object based on the provided schema, namespace, and name.
func (a *accessorImpl) GetUnstructured(gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	u, err := a.dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get resource %v of type %v: %v", name, gvr, err)
	}

	return u, nil
}

// DeleteUnstructured deletes an unstructured k8s resource object based on the provided schema, namespace, and name.
func (a *accessorImpl) DeleteUnstructured(gvr schema.GroupVersionResource, namespace, name string) error {
	if err := a.dynamicClient.Resource(gvr).Namespace(namespace).Delete(context.TODO(), name, kubeApiMeta.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete resource %v of type %v: %v", name, gvr, err)
	}
	return nil
}

// ApplyContents applies the given config contents using kubectl.
func (a *accessorImpl) ApplyContents(namespace string, contents string) ([]string, error) {
	return a.applyContents(namespace, contents, false)
}

// ApplyContentsDryRun applies the given config contents using kubectl with DryRun mode.
func (a *accessorImpl) ApplyContentsDryRun(namespace string, contents string) ([]string, error) {
	return a.applyContents(namespace, contents, true)
}

// Apply applies the config in the given filename using kubectl.
func (a *accessorImpl) Apply(namespace string, filename string) error {
	return a.apply(namespace, filename, false)
}

// ApplyDryRun applies the config in the given filename using kubectl with DryRun mode.
func (a *accessorImpl) ApplyDryRun(namespace string, filename string) error {
	return a.apply(namespace, filename, true)
}

// applyContents applies the given config contents using kubectl.
func (a *accessorImpl) applyContents(namespace string, contents string, dryRun bool) ([]string, error) {
	files, err := a.contentsToFileList(contents, "accessor_applyc")
	if err != nil {
		return nil, err
	}

	if err := a.applyInternal(namespace, files, dryRun); err != nil {
		return nil, err
	}

	return files, nil
}

// apply the config in the given filename using kubectl.
func (a *accessorImpl) apply(namespace string, filename string, dryRun bool) error {
	files, err := a.fileToFileList(filename)
	if err != nil {
		return err
	}

	return a.applyInternal(namespace, files, dryRun)
}

func (a *accessorImpl) applyInternal(namespace string, files []string, dryRun bool) error {
	for _, f := range removeEmptyFiles(files) {
		if err := a.applyFile(namespace, f, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func (a *accessorImpl) applyFile(namespace string, file string, dryRun bool) error {
	if dryRun {
		scopes.Framework.Infof("Applying YAML file (DryRun mode): %v", file)
	} else {
		scopes.Framework.Infof("Applying YAML file: %v", file)
	}

	dynamicClient, err := a.clientFactory.DynamicClient()
	if err != nil {
		return err
	}
	discoveryClient, err := a.clientFactory.ToDiscoveryClient()
	if err != nil {
		return err
	}

	// Create the options.
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()
	opts := apply.NewApplyOptions(streams)
	opts.DynamicClient = dynamicClient
	opts.DryRunVerifier = resource.NewDryRunVerifier(dynamicClient, discoveryClient)
	opts.FieldManager = "kubectl"
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
		opts.Namespace, opts.EnforceNamespace, err = a.clientFactory.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}
	}

	opts.DeleteFlags.FileNameFlags.Filenames = &[]string{file}
	opts.DeleteOptions = &delete.DeleteOptions{
		DynamicClient:   dynamicClient,
		IOStreams:       streams,
		FilenameOptions: opts.DeleteFlags.FileNameFlags.ToOptions(),
	}

	opts.OpenAPISchema, _ = a.clientFactory.OpenAPISchema()

	opts.Validator, err = a.clientFactory.Validator(true)
	if err != nil {
		return err
	}
	opts.Builder = a.clientFactory.NewBuilder()
	opts.Mapper, err = a.clientFactory.ToRESTMapper()
	if err != nil {
		return err
	}

	opts.PostProcessorFn = opts.PrintAndPrunePostProcessor()

	if err := opts.Run(); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		scopes.Framework.Infof("(FAILED) Executing kubectl apply: %s (err: %v): %s", file, err, s)
		return fmt.Errorf("%v: %s", err, s)
	}
	return nil
}

// DeleteContents deletes the given config contents using kubectl.
func (a *accessorImpl) DeleteContents(namespace string, contents string) error {
	files, err := a.contentsToFileList(contents, "accessor_deletec")
	if err != nil {
		return err
	}

	return a.deleteInternal(namespace, files)
}

// Delete the config in the given filename using kubectl.
func (a *accessorImpl) Delete(namespace string, filename string) error {
	files, err := a.fileToFileList(filename)
	if err != nil {
		return err
	}

	return a.deleteInternal(namespace, files)
}

func (a *accessorImpl) deleteInternal(namespace string, files []string) (err error) {
	for _, f := range removeEmptyFiles(files) {
		err = multierror.Append(err, a.deleteFile(namespace, f)).ErrorOrNil()
	}
	return err
}

func (a *accessorImpl) deleteFile(namespace string, file string) error {
	scopes.Framework.Infof("Deleting YAML file: %v", file)
	// Create the options.
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()

	cmdNamespace, enforceNamespace, err := a.clientFactory.ToRawKubeConfigLoader().Namespace()
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

	dynamicClient, err := a.clientFactory.DynamicClient()
	if err != nil {
		return err
	}
	discoveryClient, err := a.clientFactory.ToDiscoveryClient()
	if err != nil {
		return err
	}
	opts := delete.DeleteOptions{
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

	r := a.clientFactory.NewBuilder().
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

	opts.Mapper, err = a.clientFactory.ToRESTMapper()
	if err != nil {
		return err
	}

	if err := opts.RunDelete(a.clientFactory); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		scopes.Framework.Infof("(FAILED) Executing kubectl delete: %s (err: %v): %s", file, err, s)
		return fmt.Errorf("%v: %s", err, s)
	}
	return nil
}

// Logs calls the logs command for the specified pod, with -c, if container is specified.
func (a *accessorImpl) Logs(namespace string, pod string, container string, previousLog bool) (string, error) {
	opts := &kubeApiCore.PodLogOptions{
		Container: container,
		Previous:  previousLog,
	}
	res, err := a.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(context.TODO())
	if err != nil {
		return "", err
	}
	defer func() {
		_ = res.Close()
	}()

	builder := &strings.Builder{}
	if _, err = io.Copy(builder, res); err != nil {
		return "", err
	}

	return builder.String(), nil
}

// Exec executes the provided command on the specified pod/container.
func (a *accessorImpl) Exec(namespace, podName, containerName, command string) (string, error) {
	pod, err := a.CoreV1().Pods(namespace).Get(context.TODO(),
		podName, kubeApiMeta.GetOptions{})
	if err != nil {
		return "", err
	}

	if pod.Status.Phase == kubeApiCore.PodSucceeded || pod.Status.Phase == kubeApiCore.PodFailed {
		return "", fmt.Errorf("cannot exec into a container in a completed pod; current phase is %s", pod.Status.Phase)
	}

	if len(containerName) == 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	commandFields := strings.Fields(command)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	restConfig, err := a.clientFactory.ToRESTConfig()
	if err != nil {
		return "", err
	}

	restClient, err := a.clientFactory.RESTClient()
	if err != nil {
		return "", err
	}

	request := restClient.
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec").
		VersionedParams(&kubeApiCore.PodExecOptions{
			Container: containerName,
			Command:   commandFields,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	wrapper, upgrader, err := roundTripperFor(restConfig)
	if err != nil {
		return "", err
	}
	exec, err := remotecommand.NewSPDYExecutorForTransports(wrapper, upgrader, "POST", request.URL())
	if err != nil {
		return "", err
	}
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})

	combined := stdout.String() + stderr.String()
	if err != nil {
		scopes.Framework.Infof("(FAILED) Executing kubectl exec (ns: %s, pod: %s, container: %s, command: %s: %v: %s",
			namespace, podName, containerName, command, err, combined)
		return combined, err
	}

	return combined, nil
}

// getWorkDir lazy-creates the working directory for the accessor.
func (a *accessorImpl) getWorkDir() (string, error) {
	a.workDirMutex.Lock()
	defer a.workDirMutex.Unlock()

	workDir := a.workDir
	if workDir == "" {
		var err error
		if workDir, err = ioutil.TempDir(a.baseDir, workDirPrefix); err != nil {
			return "", err
		}
		a.workDir = workDir
	}
	return workDir, nil
}

func (a *accessorImpl) fileToFileList(filename string) ([]string, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	files, err := a.splitContentsToFiles(string(content), filenameWithoutExtension(filename))
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		files = append(files, filename)
	}

	return files, nil
}

func filenameWithoutExtension(fullPath string) string {
	_, f := filepath.Split(fullPath)
	return strings.TrimSuffix(f, filepath.Ext(fullPath))
}

func (a *accessorImpl) contentsToFileList(contents, filenamePrefix string) ([]string, error) {
	files, err := a.splitContentsToFiles(contents, filenamePrefix)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		f, err := a.writeContentsToTempFile(contents)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func (a *accessorImpl) writeContentsToTempFile(contents string) (filename string, err error) {
	defer func() {
		if err != nil && filename != "" {
			_ = os.Remove(filename)
			filename = ""
		}
	}()

	var workdir string
	workdir, err = a.getWorkDir()
	if err != nil {
		return
	}

	var f *os.File
	f, err = ioutil.TempFile(workdir, "accessor_")
	if err != nil {
		return
	}
	filename = f.Name()

	_, err = f.WriteString(contents)
	return
}

func (a *accessorImpl) splitContentsToFiles(content, filenamePrefix string) ([]string, error) {
	cfgs := yml.SplitString(content)

	namespacesAndCrds := &yamlDoc{
		docType: namespacesAndCRDs,
	}
	misc := &yamlDoc{
		docType: misc,
	}
	for _, cfg := range cfgs {
		var typeMeta kubeApiMeta.TypeMeta
		if e := yaml.Unmarshal([]byte(cfg), &typeMeta); e != nil {
			// Ignore invalid parts. This most commonly happens when it's empty or contains only comments.
			continue
		}

		switch typeMeta.Kind {
		case "Namespace":
			namespacesAndCrds.append(cfg)
		case "CustomResourceDefinition":
			namespacesAndCrds.append(cfg)
		default:
			misc.append(cfg)
		}
	}

	// If all elements were put into a single doc just return an empty list, indicating that the original
	// content should be used.
	docs := []*yamlDoc{namespacesAndCrds, misc}
	for _, doc := range docs {
		if len(doc.content) == 0 {
			return make([]string, 0), nil
		}
	}

	filesToApply := make([]string, 0, len(docs))
	for _, doc := range docs {
		workDir, err := a.getWorkDir()
		if err != nil {
			return nil, err
		}

		tfile, err := doc.toTempFile(workDir, filenamePrefix)
		if err != nil {
			return nil, err
		}
		filesToApply = append(filesToApply, tfile)
	}
	return filesToApply, nil
}

// CheckPodReady returns nil if the given pod and all of its containers are ready.
func CheckPodReady(pod *kubeApiCore.Pod) error {
	switch pod.Status.Phase {
	case kubeApiCore.PodSucceeded:
		return nil
	case kubeApiCore.PodRunning:
		// Wait until all containers are ready.
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				return fmt.Errorf("container not ready: '%s'", containerStatus.Name)
			}
		}
		if len(pod.Status.Conditions) > 0 {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == kubeApiCore.PodReady && condition.Status != kubeApiCore.ConditionTrue {
					return fmt.Errorf("pod not ready, condition message: %v", condition.Message)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

func deleteOptionsForeground() *kubeApiMeta.DeleteOptions {
	propagationPolicy := kubeApiMeta.DeletePropagationForeground
	gracePeriod := int64(0)
	return &kubeApiMeta.DeleteOptions{
		PropagationPolicy:  &propagationPolicy,
		GracePeriodSeconds: &gracePeriod,
	}
}

func newRetryOptions(opts ...retry.Option) []retry.Option {
	out := make([]retry.Option, 0, 2+len(opts))
	out = append(out, defaultRetryTimeout, defaultRetryDelay)
	out = append(out, opts...)
	return out
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
		scopes.Framework.Warnf("Error stating YAML file %s: %v", f, err)
		return true
	}
	if fileInfo.Size() == 0 {
		scopes.Framework.Warnf("Unable to process empty YAML file: %s", f)
		return true
	}
	return false
}

type docType string

const (
	namespacesAndCRDs docType = "namespaces_and_crds"
	misc              docType = "misc"
)

type yamlDoc struct {
	content string
	docType docType
}

func (d *yamlDoc) append(c string) {
	d.content = yml.JoinString(d.content, c)
}

func (d *yamlDoc) toTempFile(workDir, fileNamePrefix string) (string, error) {
	f, err := ioutil.TempFile(workDir, fmt.Sprintf("%s_%s.yaml", fileNamePrefix, d.docType))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	name := f.Name()

	_, err = f.WriteString(d.content)
	if err != nil {
		return "", err
	}
	return name, nil
}
