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

/*
Copyright 2014 The Kubernetes Authors.

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

package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/klog"

	"istio.io/istio/pkg/test/scopes"
)

const (
	// Waiting forever, set the timeout to a week.
	deleteTimeout = 168 * time.Hour
)

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
	// Create the output streams.
	_, _, stdout, stderr := genericclioptions.NewTestIOStreams()

	namespaceToUse := "default"
	enforceNamespace := false
	if len(namespace) > 0 {
		namespaceToUse = namespace
		enforceNamespace = true
	}
	filenameOptions := &resource.FilenameOptions{
		Filenames: []string{file},
	}
	r := resource.NewBuilder(a.clientGetter).
		Unstructured().
		ContinueOnError().
		NamespaceParam(namespaceToUse).DefaultNamespace().
		FilenameParam(enforceNamespace, filenameOptions).
		RequireObject(false).
		Flatten().
		Do()
	err := r.Err()
	if err != nil {
		return err
	}

	found := 0
	r = r.IgnoreErrors(kubeErrors.IsNotFound)

	warnClusterScope := enforceNamespace
	deletedInfos := []*resource.Info{}
	uidMap := make(uidMap)
	err = r.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		deletedInfos = append(deletedInfos, info)
		found++

		options := &kubeApiMeta.DeleteOptions{}
		policy := kubeApiMeta.DeletePropagationBackground
		options.PropagationPolicy = &policy

		if warnClusterScope && info.Mapping.Scope.Name() == meta.RESTScopeNameRoot {
			_, _ = fmt.Fprintf(stderr, "warning: deleting cluster-scoped resources, not scoped to the provided namespace\n")
			warnClusterScope = false
		}

		response, err := deleteResource(info, options, stdout)
		if err != nil {
			return err
		}
		resourceLocation := resourceLocation{
			GroupResource: info.Mapping.Resource.GroupResource(),
			Namespace:     info.Namespace,
			Name:          info.Name,
		}
		if status, ok := response.(*kubeApiMeta.Status); ok && status.Details != nil {
			uidMap[resourceLocation] = status.Details.UID
			return nil
		}
		responseMetadata, err := meta.Accessor(response)
		if err != nil {
			// we don't have UID, but we didn't fail the delete, next best thing is just skipping the UID
			klog.V(1).Info(err)
			return nil
		}
		uidMap[resourceLocation] = responseMetadata.GetUID()

		return nil
	})
	if err != nil {
		return err
	}
	if found == 0 {
		_, _ = fmt.Fprintf(stdout, "No resources found\n")
		return nil
	}

	visitCount := 0
	resourceFinder := genericclioptions.ResourceFinderForResult(resource.InfoListVisitor(deletedInfos))
	err = resourceFinder.Do().Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}

		visitCount++
		finalObject, success, err := a.isDeleted(info, uidMap, stderr)
		if success {
			return nil
		}
		if err == nil {
			return fmt.Errorf("%v unsatisified for unknown reason", finalObject)
		}
		return err
	})
	if err != nil {
		return err
	}
	if visitCount == 0 {
		return errors.New("no matching resources found")
	}
	if kubeErrors.IsForbidden(err) || kubeErrors.IsMethodNotSupported(err) {
		// if we're forbidden from waiting, we shouldn't fail.
		// if the resource doesn't support a verb we need, we shouldn't fail.
		scopes.Framework.Infoa(err)
		return nil
	}
	return err
}

// isDeleted is a condition func for waiting for something to be deleted
func (a *accessorImpl) isDeleted(info *resource.Info, uidMap uidMap, stderr io.Writer) (runtime.Object, bool, error) {
	endTime := time.Now().Add(deleteTimeout)
	for {
		if len(info.Name) == 0 {
			return info.Object, false, fmt.Errorf("resource name must be provided")
		}

		nameSelector := fields.OneTermEqualSelector("metadata.name", info.Name).String()

		// List with a name field selector to get the current resourceVersion to watch from (not the object's resourceVersion)
		gottenObjList, err := a.dynamicClient.Resource(info.Mapping.Resource).Namespace(info.Namespace).
			List(context.TODO(), kubeApiMeta.ListOptions{FieldSelector: nameSelector})
		if kubeErrors.IsNotFound(err) {
			return info.Object, true, nil
		}
		if err != nil {
			// TODO this could do something slightly fancier if we wish
			return info.Object, false, err
		}
		if len(gottenObjList.Items) != 1 {
			return info.Object, true, nil
		}
		gottenObj := &gottenObjList.Items[0]
		resourceLocation := resourceLocation{
			GroupResource: info.Mapping.Resource.GroupResource(),
			Namespace:     gottenObj.GetNamespace(),
			Name:          gottenObj.GetName(),
		}
		if uid, ok := uidMap[resourceLocation]; ok {
			if gottenObj.GetUID() != uid {
				return gottenObj, true, nil
			}
		}

		watchOptions := kubeApiMeta.ListOptions{}
		watchOptions.FieldSelector = nameSelector
		watchOptions.ResourceVersion = gottenObjList.GetResourceVersion()
		objWatch, err := a.dynamicClient.Resource(info.Mapping.Resource).Namespace(info.Namespace).
			Watch(context.TODO(), watchOptions)
		if err != nil {
			return gottenObj, false, err
		}

		timeout := time.Until(endTime)
		errWaitTimeoutWithName := extendErrWaitTimeout(wait.ErrWaitTimeout, info)
		if timeout < 0 {
			// we're out of time
			return gottenObj, false, errWaitTimeoutWithName
		}

		ctx, cancel := watchtools.ContextWithOptionalTimeout(context.Background(), timeout)
		watchEvent, err := watchtools.UntilWithoutRetry(ctx, objWatch, Wait{errOut: stderr}.IsDeleted)
		cancel()
		switch {
		case err == nil:
			return watchEvent.Object, true, nil
		case err == watchtools.ErrWatchClosed:
			continue
		case err == wait.ErrWaitTimeout:
			if watchEvent != nil {
				return watchEvent.Object, false, errWaitTimeoutWithName
			}
			return gottenObj, false, errWaitTimeoutWithName
		default:
			return gottenObj, false, err
		}
	}
}

func extendErrWaitTimeout(err error, info *resource.Info) error {
	return fmt.Errorf("%s on %s/%s", err.Error(), info.Mapping.Resource.Resource, info.Name)
}

// uidMap maps resourceLocation with UID
type uidMap map[resourceLocation]types.UID

// resourceLocation holds the location of a resource
type resourceLocation struct {
	GroupResource schema.GroupResource
	Namespace     string
	Name          string
}

// Wait has helper methods for handling watches, including error handling.
type Wait struct {
	errOut io.Writer
}

// IsDeleted returns true if the object is deleted. It prints any errors it encounters.
func (w Wait) IsDeleted(event watch.Event) (bool, error) {
	switch event.Type {
	case watch.Error:
		// keep waiting in the event we see an error - we expect the watch to be closed by
		// the server if the error is unrecoverable.
		err := kubeErrors.FromObject(event.Object)
		_, _ = fmt.Fprintf(w.errOut, "error: An error occurred while waiting for the object to be deleted: %v", err)
		return false, nil
	case watch.Deleted:
		return true, nil
	default:
		return false, nil
	}
}

func deleteResource(info *resource.Info, deleteOptions *kubeApiMeta.DeleteOptions,
	stdout io.Writer) (runtime.Object, error) {
	deleteResponse, err := resource.
		NewHelper(info.Client, info.Mapping).
		DeleteWithOptions(info.Namespace, info.Name, deleteOptions)
	if err != nil {
		return nil, addSourceToErr("deleting", info.Source, err)
	}

	printDeletedObject(info, stdout)
	return deleteResponse, nil
}

func printDeletedObject(info *resource.Info, out io.Writer) {
	operation := "deleted"
	groupKind := info.Mapping.GroupVersionKind
	kindString := fmt.Sprintf("%s.%s", strings.ToLower(groupKind.Kind), groupKind.Group)
	if len(groupKind.Group) == 0 {
		kindString = strings.ToLower(groupKind.Kind)
	}

	_, _ = fmt.Fprintf(out, "%s \"%s\" %s\n", kindString, info.Name, operation)
}
