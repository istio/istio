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

package files_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	krtfiles "istio.io/istio/pkg/kube/krt/files"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
)

func TestFilesCollection(t *testing.T) {
	stop := test.NewStop(t)
	root := t.TempDir()

	file.WriteOrFail(t, filepath.Join(root, "cm.yaml"), []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  namespace: default`))

	fw, err := krtfiles.NewFolderWatch[controllers.Object](root, func(b []byte) ([]controllers.Object, error) {
		return parseInputs(bytes.NewReader(b))
	}, stop)
	assert.NoError(t, err)

	col := newKubernetesFromFiles[*v1.ConfigMap](fw, krt.WithStop(stop))
	col.WaitUntilSynced(test.NewStop(t))
	tt := assert.NewTracker[string](t)
	col.Register(TrackerHandler[*v1.ConfigMap](tt))
	tt.WaitOrdered("add/default/test")

	file.WriteOrFail(t, filepath.Join(root, "cm2.yaml"), []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test2
  namespace: default`))
	tt.WaitOrdered("add/default/test2")

	file.WriteOrFail(t, filepath.Join(root, "cm.yaml"), []byte(``))
	tt.WaitOrdered("delete/default/test")
}

func newKubernetesFromFiles[T controllers.Object](fw *krtfiles.FolderWatch[controllers.Object], opts ...krt.CollectionOption) krt.Collection[T] {
	return krtfiles.NewFileCollection[controllers.Object, T](fw, func(f controllers.Object) *T {
		if t, ok := f.(T); ok {
			return &t
		}
		return nil
	}, opts...)
}

func parseInputs(f io.Reader) ([]controllers.Object, error) {
	codecs := serializer.NewCodecFactory(kube.IstioScheme)
	deserializer := codecs.UniversalDeserializer()

	reader := yamlutil.NewYAMLReader(bufio.NewReader(f))

	g := &errgroup.Group{}
	g.SetLimit(goruntime.GOMAXPROCS(0))
	resp := []controllers.Object{}
	for {
		chunk, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		resp = append(resp, nil)
		idx := len(resp) - 1
		// This is obnoxious but YAML parsing really is a bottleneck on this!
		g.Go(func() error {
			// TODO: this doubles parsing costs
			gvk, err := yamlserializer.DefaultMetaFactory.Interpret(chunk)
			if err != nil {
				return err
			}

			var obj runtime.Object

			// We want to normalize types to a single version
			s, exists := collections.PilotGatewayAPI().FindByGroupVersionAliasesKind(resource.FromKubernetesGVK(gvk))
			if exists {
				raw, err := kube.IstioScheme.New(s.GroupVersionKind().Kubernetes())
				if err != nil {
					return err
				}
				if err := yaml.Unmarshal(chunk, &raw); err != nil {
					return err
				}
				tm, err := apimeta.TypeAccessor(raw)
				if err != nil {
					return err
				}
				tm.SetAPIVersion(s.APIVersion())
				obj = raw
			} else {
				obj, _, err = deserializer.Decode(chunk, nil, obj)
				if err != nil {
					if strings.Contains(err.Error(), "no kind") {
						return nil
					}
					return fmt.Errorf("cannot parse message: %v", err)
				}
			}

			resp[idx] = obj.(controllers.Object)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	resp = slices.FilterInPlace(resp, func(object controllers.Object) bool {
		return object != nil
	})

	return resp, nil
}

func TrackerHandler[T any](tracker *assert.Tracker[string]) func(krt.Event[T]) {
	return func(o krt.Event[T]) {
		tracker.Record(fmt.Sprintf("%v/%v", o.Event, krt.GetKey(o.Latest())))
	}
}
