// Copyright 2017 Istio Authors
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

package inject

import (
	"bufio"
	"fmt"
	"io"
	"reflect"

	"istio.io/istio/pkg/log"

	"github.com/ghodss/yaml"
	// TODO(nmittler): Remove this
	_ "github.com/golang/glog"
	"k8s.io/api/batch/v2alpha1"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
)

func intoObject(c *Config, in runtime.Object) (interface{}, error) {
	out := in.DeepCopyObject()

	var metadata *metav1.ObjectMeta
	var podSpec *v1.PodSpec

	// CronJobs have JobTemplates in them, instead of Templates, so we
	// special case them.
	if job, ok := out.(*v2alpha1.CronJob); ok {
		metadata = &job.Spec.JobTemplate.ObjectMeta
		podSpec = &job.Spec.JobTemplate.Spec.Template.Spec
	} else {
		// `in` is a pointer to an Object. Dereference it.
		outValue := reflect.ValueOf(out).Elem()

		templateValue := outValue.FieldByName("Spec").FieldByName("Template")
		// `Template` is defined as a pointer in some older API
		// definitions, e.g. ReplicationController
		if templateValue.Kind() == reflect.Ptr {
			templateValue = templateValue.Elem()
		}
		metadata = templateValue.FieldByName("ObjectMeta").Addr().Interface().(*metav1.ObjectMeta)
		podSpec = templateValue.FieldByName("Spec").Addr().Interface().(*v1.PodSpec)
	}

	if !injectRequired(ignoredNamespaces, c.Policy, podSpec, metadata) {
		log.Infof("Skipping %s/%s due to policy check", metadata.Namespace, metadata.Name)
		return out, nil
	}

	sc, status, err := injectionData(&c.Params, podSpec, metadata)
	if err != nil {
		return nil, err
	}

	podSpec.InitContainers = append(podSpec.InitContainers, sc.InitContainers...)
	podSpec.Containers = append(podSpec.Containers, sc.Containers...)
	podSpec.Volumes = append(podSpec.Volumes, sc.Volumes...)

	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[istioSidecarAnnotationStatusKey] = status

	return out, nil
}

// IntoResourceFile injects the istio proxy into the specified
// kubernetes YAML file.
func IntoResourceFile(c *Config, in io.Reader, out io.Writer) error {
	reader := yamlDecoder.NewYAMLReader(bufio.NewReaderSize(in, 4096))
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		var typeMeta metav1.TypeMeta
		if err = yaml.Unmarshal(raw, &typeMeta); err != nil {
			return err
		}

		gvk := schema.FromAPIVersionAndKind(typeMeta.APIVersion, typeMeta.Kind)
		obj, err := injectScheme.New(gvk)
		var updated []byte
		if err == nil {
			if err = yaml.Unmarshal(raw, obj); err != nil {
				return err
			}
			out, err := intoObject(c, obj) // nolint: vetshadow
			if err != nil {
				return err
			}
			if updated, err = yaml.Marshal(out); err != nil {
				return err
			}
		} else {
			updated = raw // unchanged
		}
		if _, err = out.Write(updated); err != nil {
			return err
		}
		if _, err = fmt.Fprint(out, "---\n"); err != nil {
			return err
		}
	}
	return nil
}
