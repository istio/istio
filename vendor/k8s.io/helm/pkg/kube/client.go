/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube // import "k8s.io/helm/pkg/kube"

import (
	"bytes"
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/api/meta"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/evanphx/json-patch"
	appsv1 "k8s.io/api/apps/v1"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	batch "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"
	"k8s.io/client-go/kubernetes/scheme"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/kubectl/cmd/get"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
	"k8s.io/kubernetes/pkg/kubectl/validation"
)

// MissingGetHeader is added to Get's output when a resource is not found.
const MissingGetHeader = "==> MISSING\nKIND\t\tNAME\n"

// ErrNoObjectsVisited indicates that during a visit operation, no matching objects were found.
var ErrNoObjectsVisited = goerrors.New("no objects visited")

var metadataAccessor = meta.NewAccessor()

// Client represents a client capable of communicating with the Kubernetes API.
type Client struct {
	cmdutil.Factory
	Log func(string, ...interface{})
}

// New creates a new Client.
func New(getter genericclioptions.RESTClientGetter) *Client {
	if getter == nil {
		getter = genericclioptions.NewConfigFlags()
	}
	return &Client{
		Factory: cmdutil.NewFactory(getter),
		Log:     nopLogger,
	}
}

var nopLogger = func(_ string, _ ...interface{}) {}

// ResourceActorFunc performs an action on a single resource.
type ResourceActorFunc func(*resource.Info) error

// Create creates Kubernetes resources from an io.reader.
//
// Namespace will set the namespace.
func (c *Client) Create(namespace string, reader io.Reader, timeout int64, shouldWait bool) error {
	client, err := c.KubernetesClientSet()
	if err != nil {
		return err
	}
	if err := ensureNamespace(client, namespace); err != nil {
		return err
	}
	c.Log("building resources from manifest")
	infos, buildErr := c.BuildUnstructured(namespace, reader)
	if buildErr != nil {
		return buildErr
	}
	c.Log("creating %d resource(s)", len(infos))
	if err := perform(infos, createResource); err != nil {
		return err
	}
	if shouldWait {
		return c.waitForResources(time.Duration(timeout)*time.Second, infos)
	}
	return nil
}

func (c *Client) newBuilder(namespace string, reader io.Reader) *resource.Result {
	return c.NewBuilder().
		ContinueOnError().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		Schema(c.validator()).
		NamespaceParam(namespace).
		DefaultNamespace().
		Stream(reader, "").
		Flatten().
		Do()
}

func (c *Client) validator() validation.Schema {
	schema, err := c.Validator(true)
	if err != nil {
		c.Log("warning: failed to load schema: %s", err)
	}
	return schema
}

// BuildUnstructured validates for Kubernetes objects and returns unstructured infos.
func (c *Client) BuildUnstructured(namespace string, reader io.Reader) (Result, error) {
	var result Result

	result, err := c.NewBuilder().
		Unstructured().
		ContinueOnError().
		NamespaceParam(namespace).
		DefaultNamespace().
		Stream(reader, "").
		Schema(c.validator()).
		Flatten().
		Do().Infos()
	return result, scrubValidationError(err)
}

// Build validates for Kubernetes objects and returns resource Infos from a io.Reader.
func (c *Client) Build(namespace string, reader io.Reader) (Result, error) {
	var result Result
	result, err := c.newBuilder(namespace, reader).Infos()
	return result, scrubValidationError(err)
}

// Return the resource info as internal
func resourceInfoToObject(info *resource.Info, c *Client) runtime.Object {
	internalObj, err := asInternal(info)
	if err != nil {
		// If the problem is just that the resource is not registered, don't print any
		// error. This is normal for custom resources.
		if !runtime.IsNotRegisteredError(err) {
			c.Log("Warning: conversion to internal type failed: %v", err)
		}
		// Add the unstructured object in this situation. It will still get listed, just
		// with less information.
		return info.Object
	}

	return internalObj
}

func sortByKey(objs map[string](map[string]runtime.Object)) []string {
	var keys []string
	// Create a simple slice, so we can sort it
	for key := range objs {
		keys = append(keys, key)
	}
	// Sort alphabetically by version/kind keys
	sort.Strings(keys)
	return keys
}

// Get gets Kubernetes resources as pretty-printed string.
//
// Namespace will set the namespace.
func (c *Client) Get(namespace string, reader io.Reader) (string, error) {
	// Since we don't know what order the objects come in, let's group them by the types and then sort them, so
	// that when we print them, they come out looking good (headers apply to subgroups, etc.).
	objs := make(map[string](map[string]runtime.Object))
	infos, err := c.BuildUnstructured(namespace, reader)
	if err != nil {
		return "", err
	}

	var objPods = make(map[string][]v1.Pod)

	missing := []string{}
	err = perform(infos, func(info *resource.Info) error {
		c.Log("Doing get for %s: %q", info.Mapping.GroupVersionKind.Kind, info.Name)
		if err := info.Get(); err != nil {
			c.Log("WARNING: Failed Get for resource %q: %s", info.Name, err)
			missing = append(missing, fmt.Sprintf("%v\t\t%s", info.Mapping.Resource, info.Name))
			return nil
		}

		// Use APIVersion/Kind as grouping mechanism. I'm not sure if you can have multiple
		// versions per cluster, but this certainly won't hurt anything, so let's be safe.
		gvk := info.ResourceMapping().GroupVersionKind
		vk := gvk.Version + "/" + gvk.Kind

		// Initialize map. The main map groups resources based on version/kind
		// The second level is a simple 'Name' to 'Object', that will help sort
		// the individual resource later
		if objs[vk] == nil {
			objs[vk] = make(map[string]runtime.Object)
		}
		// Map between the resource name to the underlying info object
		objs[vk][info.Name] = resourceInfoToObject(info, c)

		//Get the relation pods
		objPods, err = c.getSelectRelationPod(info, objPods)
		if err != nil {
			c.Log("Warning: get the relation pod is failed, err:%s", err.Error())
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	//here, we will add the objPods to the objs
	for key, podItems := range objPods {
		for i := range podItems {
			pod := &core.Pod{}

			legacyscheme.Scheme.Convert(&podItems[i], pod, nil)
			if objs[key+"(related)"] == nil {
				objs[key+"(related)"] = make(map[string]runtime.Object)
			}
			objs[key+"(related)"][pod.ObjectMeta.Name] = runtime.Object(pod)
		}
	}

	// Ok, now we have all the objects grouped by types (say, by v1/Pod, v1/Service, etc.), so
	// spin through them and print them. Printer is cool since it prints the header only when
	// an object type changes, so we can just rely on that. Problem is it doesn't seem to keep
	// track of tab widths.
	buf := new(bytes.Buffer)
	printFlags := get.NewHumanPrintFlags()

	// Sort alphabetically by version/kind keys
	vkKeys := sortByKey(objs)
	// Iterate on sorted version/kind types
	for _, t := range vkKeys {
		if _, err = fmt.Fprintf(buf, "==> %s\n", t); err != nil {
			return "", err
		}
		typePrinter, _ := printFlags.ToPrinter("")

		var sortedResources []string
		for resource := range objs[t] {
			sortedResources = append(sortedResources, resource)
		}
		sort.Strings(sortedResources)

		// Now that each individual resource within the specific version/kind
		// is sorted, we print each resource using the k8s printer
		vk := objs[t]
		for _, resourceName := range sortedResources {
			if err := typePrinter.PrintObj(vk[resourceName], buf); err != nil {
				c.Log("failed to print object type %s, object: %q :\n %v", t, resourceName, err)
				return "", err
			}
		}
		if _, err := buf.WriteString("\n"); err != nil {
			return "", err
		}
	}
	if len(missing) > 0 {
		buf.WriteString(MissingGetHeader)
		for _, s := range missing {
			fmt.Fprintln(buf, s)
		}
	}
	return buf.String(), nil
}

// Update reads in the current configuration and a target configuration from io.reader
// and creates resources that don't already exists, updates resources that have been modified
// in the target configuration and deletes resources from the current configuration that are
// not present in the target configuration.
//
// Namespace will set the namespaces.
func (c *Client) Update(namespace string, originalReader, targetReader io.Reader, force bool, recreate bool, timeout int64, shouldWait bool) error {
	original, err := c.BuildUnstructured(namespace, originalReader)
	if err != nil {
		return fmt.Errorf("failed decoding reader into objects: %s", err)
	}

	c.Log("building resources from updated manifest")
	target, err := c.BuildUnstructured(namespace, targetReader)
	if err != nil {
		return fmt.Errorf("failed decoding reader into objects: %s", err)
	}

	updateErrors := []string{}

	c.Log("checking %d resources for changes", len(target))
	err = target.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}

		helper := resource.NewHelper(info.Client, info.Mapping)
		if _, err := helper.Get(info.Namespace, info.Name, info.Export); err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("Could not get information about the resource: %s", err)
			}

			// Since the resource does not exist, create it.
			if err := createResource(info); err != nil {
				return fmt.Errorf("failed to create resource: %s", err)
			}

			kind := info.Mapping.GroupVersionKind.Kind
			c.Log("Created a new %s called %q\n", kind, info.Name)
			return nil
		}

		originalInfo := original.Get(info)
		if originalInfo == nil {
			kind := info.Mapping.GroupVersionKind.Kind
			return fmt.Errorf("no %s with the name %q found", kind, info.Name)
		}

		if err := updateResource(c, info, originalInfo.Object, force, recreate); err != nil {
			c.Log("error updating the resource %q:\n\t %v", info.Name, err)
			updateErrors = append(updateErrors, err.Error())
		}

		return nil
	})

	switch {
	case err != nil:
		return err
	case len(updateErrors) != 0:
		return fmt.Errorf(strings.Join(updateErrors, " && "))
	}

	for _, info := range original.Difference(target) {
		c.Log("Deleting %q in %s...", info.Name, info.Namespace)

		if err := info.Get(); err != nil {
			c.Log("Unable to get obj %q, err: %s", info.Name, err)
		}
		annotations, err := metadataAccessor.Annotations(info.Object)
		if err != nil {
			c.Log("Unable to get annotations on %q, err: %s", info.Name, err)
		}
		if annotations != nil && annotations[ResourcePolicyAnno] == KeepPolicy {
			c.Log("Skipping delete of %q due to annotation [%s=%s]", info.Name, ResourcePolicyAnno, KeepPolicy)
			continue
		}

		if err := deleteResource(info); err != nil {
			c.Log("Failed to delete %q, err: %s", info.Name, err)
		}
	}
	if shouldWait {
		return c.waitForResources(time.Duration(timeout)*time.Second, target)
	}
	return nil
}

// Delete deletes Kubernetes resources from an io.reader.
//
// Namespace will set the namespace.
func (c *Client) Delete(namespace string, reader io.Reader) error {
	infos, err := c.BuildUnstructured(namespace, reader)
	if err != nil {
		return err
	}
	return perform(infos, func(info *resource.Info) error {
		c.Log("Starting delete for %q %s", info.Name, info.Mapping.GroupVersionKind.Kind)
		err := deleteResource(info)
		return c.skipIfNotFound(err)
	})
}

func (c *Client) skipIfNotFound(err error) error {
	if errors.IsNotFound(err) {
		c.Log("%v", err)
		return nil
	}
	return err
}

func (c *Client) watchTimeout(t time.Duration) ResourceActorFunc {
	return func(info *resource.Info) error {
		return c.watchUntilReady(t, info)
	}
}

// WatchUntilReady watches the resource given in the reader, and waits until it is ready.
//
// This function is mainly for hook implementations. It watches for a resource to
// hit a particular milestone. The milestone depends on the Kind.
//
// For most kinds, it checks to see if the resource is marked as Added or Modified
// by the Kubernetes event stream. For some kinds, it does more:
//
// - Jobs: A job is marked "Ready" when it has successfully completed. This is
//   ascertained by watching the Status fields in a job's output.
//
// Handling for other kinds will be added as necessary.
func (c *Client) WatchUntilReady(namespace string, reader io.Reader, timeout int64, shouldWait bool) error {
	infos, err := c.BuildUnstructured(namespace, reader)
	if err != nil {
		return err
	}
	// For jobs, there's also the option to do poll c.Jobs(namespace).Get():
	// https://github.com/adamreese/kubernetes/blob/master/test/e2e/job.go#L291-L300
	return perform(infos, c.watchTimeout(time.Duration(timeout)*time.Second))
}

func perform(infos Result, fn ResourceActorFunc) error {
	if len(infos) == 0 {
		return ErrNoObjectsVisited
	}

	for _, info := range infos {
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}

func createResource(info *resource.Info) error {
	obj, err := resource.NewHelper(info.Client, info.Mapping).Create(info.Namespace, true, info.Object, nil)
	if err != nil {
		return err
	}
	return info.Refresh(obj, true)
}

func deleteResource(info *resource.Info) error {
	policy := metav1.DeletePropagationBackground
	opts := &metav1.DeleteOptions{PropagationPolicy: &policy}
	_, err := resource.NewHelper(info.Client, info.Mapping).DeleteWithOptions(info.Namespace, info.Name, opts)
	return err
}

func createPatch(target *resource.Info, current runtime.Object) ([]byte, types.PatchType, error) {
	oldData, err := json.Marshal(current)
	if err != nil {
		return nil, types.StrategicMergePatchType, fmt.Errorf("serializing current configuration: %s", err)
	}
	newData, err := json.Marshal(target.Object)
	if err != nil {
		return nil, types.StrategicMergePatchType, fmt.Errorf("serializing target configuration: %s", err)
	}

	// While different objects need different merge types, the parent function
	// that calls this does not try to create a patch when the data (first
	// returned object) is nil. We can skip calculating the merge type as
	// the returned merge type is ignored.
	if apiequality.Semantic.DeepEqual(oldData, newData) {
		return nil, types.StrategicMergePatchType, nil
	}

	// Get a versioned object
	versionedObject := asVersioned(target)

	// Unstructured objects, such as CRDs, may not have an not registered error
	// returned from ConvertToVersion. Anything that's unstructured should
	// use the jsonpatch.CreateMergePatch. Strategic Merge Patch is not supported
	// on objects like CRDs.
	_, isUnstructured := versionedObject.(runtime.Unstructured)

	switch {
	case runtime.IsNotRegisteredError(err), isUnstructured:
		// fall back to generic JSON merge patch
		patch, err := jsonpatch.CreateMergePatch(oldData, newData)
		return patch, types.MergePatchType, err
	case err != nil:
		return nil, types.StrategicMergePatchType, fmt.Errorf("failed to get versionedObject: %s", err)
	default:
		patch, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, versionedObject)
		return patch, types.StrategicMergePatchType, err
	}
}

func updateResource(c *Client, target *resource.Info, currentObj runtime.Object, force bool, recreate bool) error {
	patch, patchType, err := createPatch(target, currentObj)
	if err != nil {
		return fmt.Errorf("failed to create patch: %s", err)
	}
	if patch == nil {
		c.Log("Looks like there are no changes for %s %q", target.Mapping.GroupVersionKind.Kind, target.Name)
		// This needs to happen to make sure that tiller has the latest info from the API
		// Otherwise there will be no labels and other functions that use labels will panic
		if err := target.Get(); err != nil {
			return fmt.Errorf("error trying to refresh resource information: %v", err)
		}
	} else {
		// send patch to server
		helper := resource.NewHelper(target.Client, target.Mapping)

		obj, err := helper.Patch(target.Namespace, target.Name, patchType, patch, nil)
		if err != nil {
			kind := target.Mapping.GroupVersionKind.Kind
			log.Printf("Cannot patch %s: %q (%v)", kind, target.Name, err)

			if force {
				// Attempt to delete...
				if err := deleteResource(target); err != nil {
					return err
				}
				log.Printf("Deleted %s: %q", kind, target.Name)

				// ... and recreate
				if err := createResource(target); err != nil {
					return fmt.Errorf("Failed to recreate resource: %s", err)
				}
				log.Printf("Created a new %s called %q\n", kind, target.Name)

				// No need to refresh the target, as we recreated the resource based
				// on it. In addition, it might not exist yet and a call to `Refresh`
				// may fail.
			} else {
				log.Print("Use --force to force recreation of the resource")
				return err
			}
		} else {
			// When patch succeeds without needing to recreate, refresh target.
			target.Refresh(obj, true)
		}
	}

	if !recreate {
		return nil
	}

	versioned := asVersioned(target)
	selector, ok := getSelectorFromObject(versioned)
	if !ok {
		return nil
	}

	client, err := c.KubernetesClientSet()
	if err != nil {
		return err
	}

	pods, err := client.CoreV1().Pods(target.Namespace).List(metav1.ListOptions{
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	if err != nil {
		return err
	}

	// Restart pods
	for _, pod := range pods.Items {
		c.Log("Restarting pod: %v/%v", pod.Namespace, pod.Name)

		// Delete each pod for get them restarted with changed spec.
		if err := client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, metav1.NewPreconditionDeleteOptions(string(pod.UID))); err != nil {
			return err
		}
	}
	return nil
}

func getSelectorFromObject(obj runtime.Object) (map[string]string, bool) {
	switch typed := obj.(type) {

	case *v1.ReplicationController:
		return typed.Spec.Selector, true

	case *extv1beta1.ReplicaSet:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1.ReplicaSet:
		return typed.Spec.Selector.MatchLabels, true

	case *extv1beta1.Deployment:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1beta1.Deployment:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1beta2.Deployment:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1.Deployment:
		return typed.Spec.Selector.MatchLabels, true

	case *extv1beta1.DaemonSet:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1beta2.DaemonSet:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1.DaemonSet:
		return typed.Spec.Selector.MatchLabels, true

	case *batch.Job:
		return typed.Spec.Selector.MatchLabels, true

	case *appsv1beta1.StatefulSet:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1beta2.StatefulSet:
		return typed.Spec.Selector.MatchLabels, true
	case *appsv1.StatefulSet:
		return typed.Spec.Selector.MatchLabels, true

	default:
		return nil, false
	}
}

func (c *Client) watchUntilReady(timeout time.Duration, info *resource.Info) error {
	w, err := resource.NewHelper(info.Client, info.Mapping).WatchSingle(info.Namespace, info.Name, info.ResourceVersion)
	if err != nil {
		return err
	}

	kind := info.Mapping.GroupVersionKind.Kind
	c.Log("Watching for changes to %s %s with timeout of %v", kind, info.Name, timeout)

	// What we watch for depends on the Kind.
	// - For a Job, we watch for completion.
	// - For all else, we watch until Ready.
	// In the future, we might want to add some special logic for types
	// like Ingress, Volume, etc.

	ctx, cancel := watchtools.ContextWithOptionalTimeout(context.Background(), timeout)
	defer cancel()
	_, err = watchtools.UntilWithoutRetry(ctx, w, func(e watch.Event) (bool, error) {
		switch e.Type {
		case watch.Added, watch.Modified:
			// For things like a secret or a config map, this is the best indicator
			// we get. We care mostly about jobs, where what we want to see is
			// the status go into a good state. For other types, like ReplicaSet
			// we don't really do anything to support these as hooks.
			c.Log("Add/Modify event for %s: %v", info.Name, e.Type)
			if kind == "Job" {
				return c.waitForJob(e, info.Name)
			}
			return true, nil
		case watch.Deleted:
			c.Log("Deleted event for %s", info.Name)
			return true, nil
		case watch.Error:
			// Handle error and return with an error.
			c.Log("Error event for %s", info.Name)
			return true, fmt.Errorf("Failed to deploy %s", info.Name)
		default:
			return false, nil
		}
	})
	return err
}

// waitForJob is a helper that waits for a job to complete.
//
// This operates on an event returned from a watcher.
func (c *Client) waitForJob(e watch.Event, name string) (bool, error) {
	job := &batch.Job{}
	err := legacyscheme.Scheme.Convert(e.Object, job, nil)
	if err != nil {
		return true, err
	}

	for _, c := range job.Status.Conditions {
		if c.Type == batch.JobComplete && c.Status == v1.ConditionTrue {
			return true, nil
		} else if c.Type == batch.JobFailed && c.Status == v1.ConditionTrue {
			return true, fmt.Errorf("Job failed: %s", c.Reason)
		}
	}

	c.Log("%s: Jobs active: %d, jobs failed: %d, jobs succeeded: %d", name, job.Status.Active, job.Status.Failed, job.Status.Succeeded)
	return false, nil
}

// scrubValidationError removes kubectl info from the message.
func scrubValidationError(err error) error {
	if err == nil {
		return nil
	}
	const stopValidateMessage = "if you choose to ignore these errors, turn validation off with --validate=false"

	if strings.Contains(err.Error(), stopValidateMessage) {
		return goerrors.New(strings.Replace(err.Error(), "; "+stopValidateMessage, "", -1))
	}
	return err
}

// WaitAndGetCompletedPodPhase waits up to a timeout until a pod enters a completed phase
// and returns said phase (PodSucceeded or PodFailed qualify).
func (c *Client) WaitAndGetCompletedPodPhase(namespace string, reader io.Reader, timeout time.Duration) (v1.PodPhase, error) {
	infos, err := c.Build(namespace, reader)
	if err != nil {
		return v1.PodUnknown, err
	}
	info := infos[0]

	kind := info.Mapping.GroupVersionKind.Kind
	if kind != "Pod" {
		return v1.PodUnknown, fmt.Errorf("%s is not a Pod", info.Name)
	}

	if err := c.watchPodUntilComplete(timeout, info); err != nil {
		return v1.PodUnknown, err
	}

	if err := info.Get(); err != nil {
		return v1.PodUnknown, err
	}
	status := info.Object.(*v1.Pod).Status.Phase

	return status, nil
}

func (c *Client) watchPodUntilComplete(timeout time.Duration, info *resource.Info) error {
	w, err := resource.NewHelper(info.Client, info.Mapping).WatchSingle(info.Namespace, info.Name, info.ResourceVersion)
	if err != nil {
		return err
	}

	c.Log("Watching pod %s for completion with timeout of %v", info.Name, timeout)
	ctx, cancel := watchtools.ContextWithOptionalTimeout(context.Background(), timeout)
	defer cancel()
	_, err = watchtools.UntilWithoutRetry(ctx, w, func(e watch.Event) (bool, error) {
		return isPodComplete(e)
	})

	return err
}

func isPodComplete(event watch.Event) (bool, error) {
	o, ok := event.Object.(*v1.Pod)
	if !ok {
		return true, fmt.Errorf("expected a *v1.Pod, got %T", event.Object)
	}
	if event.Type == watch.Deleted {
		return false, fmt.Errorf("pod not found")
	}
	switch o.Status.Phase {
	case v1.PodFailed, v1.PodSucceeded:
		return true, nil
	}
	return false, nil
}

//get a kubernetes resources' relation pods
// kubernetes resource used select labels to relate pods
func (c *Client) getSelectRelationPod(info *resource.Info, objPods map[string][]v1.Pod) (map[string][]v1.Pod, error) {
	if info == nil {
		return objPods, nil
	}

	c.Log("get relation pod of object: %s/%s/%s", info.Namespace, info.Mapping.GroupVersionKind.Kind, info.Name)

	versioned := asVersioned(info)
	selector, ok := getSelectorFromObject(versioned)
	if !ok {
		return objPods, nil
	}

	client, _ := c.KubernetesClientSet()

	pods, err := client.CoreV1().Pods(info.Namespace).List(metav1.ListOptions{
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	if err != nil {
		return objPods, err
	}

	for _, pod := range pods.Items {
		vk := "v1/Pod"
		if !isFoundPod(objPods[vk], pod) {
			objPods[vk] = append(objPods[vk], pod)
		}
	}
	return objPods, nil
}

func isFoundPod(podItem []v1.Pod, pod v1.Pod) bool {
	for _, value := range podItem {
		if (value.Namespace == pod.Namespace) && (value.Name == pod.Name) {
			return true
		}
	}
	return false
}

func asVersioned(info *resource.Info) runtime.Object {
	converter := runtime.ObjectConvertor(scheme.Scheme)
	groupVersioner := runtime.GroupVersioner(schema.GroupVersions(scheme.Scheme.PrioritizedVersionsAllGroups()))
	if info.Mapping != nil {
		groupVersioner = info.Mapping.GroupVersionKind.GroupVersion()
	}

	if obj, err := converter.ConvertToVersion(info.Object, groupVersioner); err == nil {
		return obj
	}
	return info.Object
}

func asInternal(info *resource.Info) (runtime.Object, error) {
	groupVersioner := info.Mapping.GroupVersionKind.GroupKind().WithVersion(runtime.APIVersionInternal).GroupVersion()
	return legacyscheme.Scheme.ConvertToVersion(info.Object, groupVersioner)
}
