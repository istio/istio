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
	"bytes"
	"fmt"
	"io"

	kubeApiCore "k8s.io/api/core/v1"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/scopes"
)

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
	s, err := a.getSchema()
	if err != nil {
		return err
	}

	for _, f := range removeEmptyFiles(files) {
		if err := a.applyFile(namespace, f, s, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func (a *accessorImpl) applyFile(namespace string, file string, s *openAPISchema, dryRun bool) error {
	if dryRun {
		scopes.Framework.Infof("Applying YAML file (DryRun mode): %v", file)
	} else {
		scopes.Framework.Infof("Applying YAML file: %v", file)
	}

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
		Schema(newValidator(s)).
		ContinueOnError().
		NamespaceParam(namespaceToUse).DefaultNamespace().
		FilenameParam(enforceNamespace, filenameOptions).
		Flatten().
		Do()
	infos, err := r.Infos()
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		return fmt.Errorf("no objects to apply")
	}

	// Iterate through all objects, applying each one.
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	for _, info := range infos {
		if err := a.applyObject(info, s, dryRun, stdout, stderr); err != nil {
			// Concatenate the stdout and stderr
			s := stdout.String() + stderr.String()
			scopes.Framework.Infof("Failed applying YAML file: %s (err: %v): %s", file, err, s)
			return fmt.Errorf("%v: %s", err, s)
		}
	}
	return nil
}

func (a *accessorImpl) applyObject(info *resource.Info, s *openAPISchema, dryRun bool, stdout io.Writer,
	stderr io.Writer) error {

	operationFor := func(op string) string {
		if dryRun {
			op += " (server dry run)"
		}
		return op
	}

	// Get the modified configuration of the object. Embed the result
	// as an annotation in the modified configuration, so that it will appear
	// in the patch sent to the server.
	modified, err := kube.GetModifiedConfiguration(info.Object, true, unstructured.UnstructuredJSONScheme)
	if err != nil {
		return addSourceToErr(fmt.Sprintf("retrieving modified configuration from:\n%s\nfor:", info.String()),
			info.Source, err)
	}

	// Get the existing object from the server.
	if err := info.Get(); err != nil {
		if !kubeErrors.IsNotFound(err) {
			return addSourceToErr(fmt.Sprintf("retrieving current configuration of:\n%s\nfrom server for:", info.String()), info.Source, err)
		}

		// Create the resource if it doesn't exist
		// First, update the annotation used by kubectl apply
		if err := kube.CreateApplyAnnotation(info.Object, unstructured.UnstructuredJSONScheme); err != nil {
			return addSourceToErr("creating", info.Source, err)
		}

		// Then create the resource and skip the three-way merge
		helper := resource.NewHelper(info.Client, info.Mapping)
		if dryRun {
			helper.DryRun(true)
		}
		obj, err := helper.Create(info.Namespace, true, info.Object)
		if err != nil {
			return addSourceToErr("creating", info.Source, err)
		}
		_ = info.Refresh(obj, true)

		if err = printOutput(operationFor("created"), info.Object, stdout); err != nil {
			return err
		}
		return nil
	}

	metadata, _ := meta.Accessor(info.Object)
	annotationMap := metadata.GetAnnotations()
	if _, ok := annotationMap[kubeApiCore.LastAppliedConfigAnnotation]; !ok {
		_, _ = fmt.Fprintf(stderr, "Warning: %[1]s apply should be used on resource created by "+
			"either %[1]s create --save-config or %[1]s apply\n", fieldManager)
	}

	patchBytes, patchedObject, err := a.patch(info, s, modified, dryRun, stderr)
	if err != nil {
		return addSourceToErr(fmt.Sprintf("applying patch:\n%s\nto:\n%v\nfor:", patchBytes, info), info.Source, err)
	}

	_ = info.Refresh(patchedObject, true)

	op := operationFor("configured")
	if string(patchBytes) == "{}" {
		op = operationFor("unchanged")
	}
	if err = printOutput(op, info.Object, stdout); err != nil {
		return err
	}
	return nil
}

func (a *accessorImpl) patch(info *resource.Info, s *openAPISchema, modified []byte, dryRun bool,
	stderr io.Writer) ([]byte, runtime.Object, error) {
	// Serialize the current configuration of the object from the server.
	current, err := runtime.Encode(unstructured.UnstructuredJSONScheme, info.Object)
	if err != nil {
		return nil, nil, addSourceToErr(fmt.Sprintf("serializing current configuration from:\n%v\nfor:",
			info.Object), info.Source, err)
	}

	// Retrieve the original configuration of the object from the annotation.
	original, err := kube.GetOriginalConfiguration(info.Object)
	if err != nil {
		return nil, nil, addSourceToErr(fmt.Sprintf("retrieving original configuration from:\n%v\nfor:",
			info.Object), info.Source, err)
	}

	var patchType types.PatchType
	var patch []byte
	createPatchErrFormat := "creating patch with:\noriginal:\n%s\nmodified:\n%s\ncurrent:\n%s\nfor:"

	// Create the versioned struct from the type defined in the restmapping
	// (which is the API version we'll be submitting the patch to)
	versionedObject, err := scheme.Scheme.New(info.Mapping.GroupVersionKind)
	switch {
	case runtime.IsNotRegisteredError(err):
		// fall back to generic JSON merge patch
		patchType = types.MergePatchType
		preconditions := []mergepatch.PreconditionFunc{mergepatch.RequireKeyUnchanged("apiVersion"),
			mergepatch.RequireKeyUnchanged("kind"), mergepatch.RequireMetadataKeyUnchanged("name")}
		patch, err = jsonmergepatch.CreateThreeWayJSONMergePatch(original, modified, current, preconditions...)
		if err != nil {
			if mergepatch.IsPreconditionFailed(err) {
				return nil, nil, fmt.Errorf("%s", "At least one of apiVersion, kind and name was changed")
			}
			return nil, nil, addSourceToErr(fmt.Sprintf(createPatchErrFormat, original, modified, current),
				info.Source, err)
		}
	case err != nil:
		return nil, nil, addSourceToErr(fmt.Sprintf("getting instance of versioned object for %v:",
			info.Mapping.GroupVersionKind), info.Source, err)
	default:
		// Compute a three way strategic merge patch to send to server.
		patchType = types.StrategicMergePatchType

		// Try to use openapi first if the openapi spec is available and can successfully calculate the patch.
		// Otherwise, fall back to baked-in types.
		if objSchema := s.LookupResource(info.Mapping.GroupVersionKind); objSchema != nil {
			lookupPatchMeta := strategicpatch.PatchMetaFromOpenAPI{Schema: objSchema}
			if openapiPatch, err := strategicpatch.CreateThreeWayMergePatch(original, modified, current,
				lookupPatchMeta, true); err != nil {
				_, _ = fmt.Fprintf(stderr, "warning: error calculating patch from openapi spec: %v\n", err)
			} else {
				patchType = types.StrategicMergePatchType
				patch = openapiPatch
			}
		}

		if patch == nil {
			lookupPatchMeta, err := strategicpatch.NewPatchMetaFromStruct(versionedObject)
			if err != nil {
				return nil, nil, addSourceToErr(fmt.Sprintf(createPatchErrFormat, original, modified, current),
					info.Source, err)
			}
			patch, err = strategicpatch.CreateThreeWayMergePatch(original, modified, current,
				lookupPatchMeta, true)
			if err != nil {
				return nil, nil, addSourceToErr(fmt.Sprintf(createPatchErrFormat, original, modified, current),
					info.Source, err)
			}
		}
	}

	helper := resource.NewHelper(info.Client, info.Mapping)
	var patchedObject runtime.Object
	if string(patch) == "{}" {
		patchedObject = info.Object
	} else {
		patchedObject, err = helper.DryRun(dryRun).Patch(info.Namespace, info.Name, patchType, patch, nil)
		if err != nil {
			return nil, nil, err
		}
	}
	return patch, patchedObject, err
}

func printOutput(operation string, obj runtime.Object, out io.Writer) error {
	p := printers.NamePrinter{
		Operation:   operation,
		ShortOutput: true,
	}
	return p.PrintObj(obj, out)
}
