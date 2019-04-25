// Copyright 2018 The Operator-SDK Authors
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

package test

import (
	goctx "context"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/ghodss/yaml"
	"github.com/operator-framework/operator-sdk/internal/util/yamlutil"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (ctx *TestCtx) GetNamespace() (string, error) {
	if ctx.namespace != "" {
		return ctx.namespace, nil
	}
	if *singleNamespace {
		ctx.namespace = Global.Namespace
		return ctx.namespace, nil
	}
	// create namespace
	ctx.namespace = ctx.GetID()
	namespaceObj := &core.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ctx.namespace}}
	_, err := Global.KubeClient.CoreV1().Namespaces().Create(namespaceObj)
	if apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("namespace %s already exists: %v", ctx.namespace, err)
	} else if err != nil {
		return "", err
	}
	ctx.AddCleanupFn(func() error {
		return Global.KubeClient.CoreV1().Namespaces().Delete(ctx.namespace, metav1.NewDeleteOptions(0))
	})
	return ctx.namespace, nil
}

func (ctx *TestCtx) createFromYAML(yamlFile []byte, skipIfExists bool, cleanupOptions *CleanupOptions) error {
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return err
	}
	scanner := yamlutil.NewYAMLScanner(yamlFile)
	for scanner.Scan() {
		yamlSpec := scanner.Bytes()

		obj := &unstructured.Unstructured{}
		jsonSpec, err := yaml.YAMLToJSON(yamlSpec)
		if err != nil {
			return fmt.Errorf("could not convert yaml file to json: %v", err)
		}
		if err := obj.UnmarshalJSON(jsonSpec); err != nil {
			return fmt.Errorf("failed to unmarshal object spec: (%v)", err)
		}
		obj.SetNamespace(namespace)
		err = Global.Client.Create(goctx.TODO(), obj, cleanupOptions)
		if skipIfExists && apierrors.IsAlreadyExists(err) {
			continue
		}
		if err != nil {
			_, restErr := restMapper.RESTMappings(obj.GetObjectKind().GroupVersionKind().GroupKind())
			if restErr == nil {
				return err
			}
			// don't store error, as only error will be timeout. Error from runtime client will be easier for
			// the user to understand than the timeout error, so just use that if we fail
			_ = wait.PollImmediate(time.Second*1, time.Second*10, func() (bool, error) {
				restMapper.Reset()
				_, err := restMapper.RESTMappings(obj.GetObjectKind().GroupVersionKind().GroupKind())
				if err != nil {
					return false, nil
				}
				return true, nil
			})
			err = Global.Client.Create(goctx.TODO(), obj, cleanupOptions)
			if skipIfExists && apierrors.IsAlreadyExists(err) {
				continue
			}
			if err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan manifest: (%v)", err)
	}
	return nil
}

func (ctx *TestCtx) InitializeClusterResources(cleanupOptions *CleanupOptions) error {
	// create namespaced resources
	namespacedYAML, err := ioutil.ReadFile(*Global.NamespacedManPath)
	if err != nil {
		return fmt.Errorf("failed to read namespaced manifest: %v", err)
	}
	return ctx.createFromYAML(namespacedYAML, false, cleanupOptions)
}
