// Copyright 2018 Istio Authors
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

package fs_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/meshconfig"
	kubeMeta "istio.io/istio/galley/pkg/metadata/kube"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/fs"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/testing/events"
	"istio.io/istio/pkg/appsignals"
	sn "istio.io/istio/pkg/mcp/snapshot"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	mixerYAML = `
apiVersion: "config.istio.io/v1alpha2"
kind: denier
metadata:
  name: some.mixer.denier
spec:
  status:
    code: 7
    message: Not allowed
---
apiVersion: "config.istio.io/v1alpha2"
kind: checknothing
metadata:
  name: some.mixer.checknothing
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: some.mixer.rule
spec:
  match: destination.labels["app"] == "someapp"
  actions:
  - handler: some.denier
    instances: [ some.checknothing ]
`

	mixerPartYAML = `
apiVersion: "config.istio.io/v1alpha2"
kind: denier
metadata:
  name: some.mixer.denier
spec:
  status:
    code: 7
    message: Not allowed
---
apiVersion: "config.istio.io/v1alpha2"
kind: checknothing
metadata:
  name: some.mixer.checknothing
spec:
`

	virtualServiceYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-for-myapp
spec:
  hosts:
  - some.example.com
  gateways:
  - some-ingress
  http:
  - route:
    - destination:
        host: some.example.internal
`

	virtualServiceChangedYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-for-myapp
spec:
  hosts:
  - someother.example.com
  gateways:
  - some-ingress
  http:
  - route:
    - destination:
        host: some.example.internal
`

	builtinYAML = `
apiVersion: v1
kind: Service
metadata:
  annotations:
    ak1: av1
  creationTimestamp: 2018-02-12T15:48:44Z
  labels:
    lk1: lv1
  name: kube-dns
  namespace: kube-system
spec:
  clusterIP: 10.43.240.10
  ports:
  - name: dns-tcp
    port: 53
    protocol: TCP
    targetPort: 53
  type: ClusterIP
`

	sameNameDifferentTypes = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: service-a
spec:
  hosts:
  - some.example.com
  gateways:
  - some-ingress
  http:
  - route:
    - destination:
        host: some.example.internal
---
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: service-a
spec:
   hosts:
   - some.example.com
   ports:
   - number: 80
     name: http
     protocol: HTTP
   resolution: STATIC
   endpoints:
    - address: 127.0.0.2
      ports:
        http: 7072
`

	cfg = &converter.Config{Mesh: meshconfig.NewInMemory()}

	runtimeScheme = k8sRuntime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func TestNew(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	_ = newOrFail(t, dir)
}

func TestInvalidDirShouldSucceed(t *testing.T) {
	s := newOrFail(t, "somebaddir")

	ch := startOrFail(t, s)
	defer s.Stop()

	expectFullSync(t, ch)
}

func TestInitialFile(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	// Copy a file to the dir
	u := copyAndParseSinglePartFile(t, dir, "virtual_services.yaml", virtualServiceYAML)

	// Start the source.
	s := newOrFail(t, dir)
	ch := startOrFail(t, s)
	defer s.Stop()

	// Expect the Add event.
	u.SetResourceVersion("v0")
	spec := *kubeMeta.Types.Get("VirtualService")
	expected := resource.Event{
		Kind:  resource.Added,
		Entry: unstructuredToEntry(t, u, spec),
	}
	actual := events.Expect(t, ch)
	g.Expect(actual).To(Equal(expected))

	// Expect the full sync event immediately after.
	expectFullSync(t, ch)
}

func TestDynamicResource(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	// Start the source.
	s := newOrFail(t, dir)
	ch := startOrFail(t, s)
	defer s.Stop()

	// Expect the full sync event.
	expectFullSync(t, ch)

	fileName := "virtual_services.yaml"
	spec := *kubeMeta.Types.Get("VirtualService")

	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Copy a file to the dir
		u := copyAndParseSinglePartFile(t, dir, fileName, virtualServiceYAML)
		u.SetResourceVersion("v1")
		appsignals.Notify("test", syscall.SIGUSR1)

		// Expect the Add event.
		expected := resource.Event{
			Kind:  resource.Added,
			Entry: unstructuredToEntry(t, u, spec),
		}
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Overwrite the original file
		u := copyAndParseSinglePartFile(t, dir, fileName, virtualServiceChangedYAML)
		u.SetResourceVersion("v2")
		appsignals.Notify("test", syscall.SIGUSR1)

		// Expect the update event.
		expected := resource.Event{
			Kind:  resource.Updated,
			Entry: unstructuredToEntry(t, u, spec),
		}
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Delete the file.
		deleteFiles(t, dir, fileName)

		u := &unstructured.Unstructured{}
		parseYaml(t, virtualServiceChangedYAML, u)
		u.SetResourceVersion("v2")
		appsignals.Notify("test", syscall.SIGUSR1)

		// Expect the update event.
		expected := resource.Event{
			Kind: resource.Deleted,
			Entry: resource.Entry{
				ID: getID(u, spec),
			},
		}
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func TestDuplicateResourceNamesDifferentTypes(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	// Copy a file to the dir
	u := copyAndParseFile(t, dir, "service-a.yaml", sameNameDifferentTypes)

	// Start the source.
	s := newOrFail(t, dir)
	ch := startOrFail(t, s)
	defer s.Stop()

	actual1 := events.Expect(t, ch)
	actual2 := events.Expect(t, ch)
	if actual2.Entry.ID.Collection == kubeMeta.Types.Get("VirtualService").Target.Collection {
		// Reorder
		actual2, actual1 = actual1, actual2
	}
	// Expect the add of VirtualService
	u[0].SetResourceVersion("v0")
	expectedVirtualService := resource.Event{
		Kind:  resource.Added,
		Entry: unstructuredToEntry(t, u[0], *kubeMeta.Types.Get("VirtualService")),
	}
	g.Expect(actual1).To(Equal(expectedVirtualService))

	// ... and the add of service entry
	u[1].SetResourceVersion("v0")
	expectedServiceEntry := resource.Event{
		Kind:  resource.Added,
		Entry: unstructuredToEntry(t, u[1], *kubeMeta.Types.Get("ServiceEntry")),
	}
	g.Expect(actual2).To(Equal(expectedServiceEntry))

	// Expect the full sync event immediately after.
	expectFullSync(t, ch)
}

func TestBuiltinResource(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	// Start the source.
	s := newOrFail(t, dir)
	ch := startOrFail(t, s)
	defer s.Stop()

	// Expect the full sync event.
	expectFullSync(t, ch)

	fileName := "services.yaml"
	spec := *kubeMeta.Types.Get("Service")
	svc := coreV1.Service{}
	parseYaml(t, builtinYAML, &svc)

	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Copy a file to the dir
		copyFile(t, dir, fileName, builtinYAML)
		appsignals.Notify("test", syscall.SIGUSR1)

		// Expect the Add event.
		svc.SetResourceVersion("v1")
		expected := resource.Event{
			Kind:  resource.Added,
			Entry: serviceToEntry(t, svc, spec),
		}
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Change the ClustIP and overwrite the original file
		oldClusterIP := "10.43.240.10"
		newClusterIP := "10.43.240.11"
		newYAML := strings.Replace(builtinYAML, oldClusterIP, newClusterIP, -1)
		copyFile(t, dir, fileName, newYAML)
		appsignals.Notify("test", syscall.SIGUSR1)

		// Expect the update event.
		svc.Spec.ClusterIP = newClusterIP
		svc.SetResourceVersion("v2")
		expected := resource.Event{
			Kind:  resource.Updated,
			Entry: serviceToEntry(t, svc, spec),
		}
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})

	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)

		// Delete the file.
		deleteFiles(t, dir, fileName)
		appsignals.Notify("test", syscall.SIGUSR1)

		// Expect the update event.
		expected := resource.Event{
			Kind: resource.Deleted,
			Entry: resource.Entry{
				ID: getID(&svc, spec),
			},
		}
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func TestMultipartEvents(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	// Copy a file to the dir
	objs := copyAndParseFile(t, dir, "mixer.yaml", mixerYAML)

	// Start the source.
	s := newOrFail(t, dir)
	ch := startOrFail(t, s)
	defer s.Stop()

	t.Run("Add", func(t *testing.T) {
		for i := 0; i < len(objs); i++ {
			t.Run(fmt.Sprintf("event_%d", i), func(t *testing.T) {
				g := NewGomegaWithT(t)

				// Read the next event from the channel.
				actual := events.Expect(t, ch)

				// Look up the object for this event.
				var obj *unstructured.Unstructured
				var spec schema.ResourceSpec
				for _, o := range objs {
					s := *kubeMeta.Types.Get(o.GetKind())
					if s.Target.Collection == actual.Entry.ID.Collection {
						obj = o
						spec = s
						break
					}
				}

				g.Expect(obj).ToNot(BeNil())

				obj.SetResourceVersion("v0")
				expected := resource.Event{
					Kind:  resource.Added,
					Entry: unstructuredToEntry(t, obj, spec),
				}
				g.Expect(actual).To(Equal(expected))
			})
		}
	})

	// Expect the full sync event immediately after.
	expectFullSync(t, ch)

	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		// Now overwrite the file which removes the last resource.
		_ = copyAndParseFile(t, dir, "mixer.yaml", mixerPartYAML)
		appsignals.Notify("test", syscall.SIGUSR1)
		obj := objs[2]
		expected := resource.Event{
			Kind: resource.Deleted,
			Entry: resource.Entry{
				ID: getID(obj, *kubeMeta.Types.Get(obj.GetKind())),
			},
		}
		actual := events.Expect(t, ch)
		g.Expect(actual).To(Equal(expected))
	})
}

func TestSnapshotDistribution(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	copyFile(t, dir, "virtual_services.yaml", virtualServiceYAML)

	// Create the source
	s := newOrFail(t, dir)

	// Create a snapshot distributor.
	d := runtime.NewInMemoryDistributor()

	// Create and start the runtime processor.
	cfg := &runtime.Config{Mesh: meshconfig.NewInMemory()}
	processor := runtime.NewProcessor(s, d, cfg)
	err := processor.Start()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer processor.Stop()

	// Wait for a snapshot to be distributed.
	ch := make(chan bool)
	listenerAction := func(sp sn.Snapshot) {
		ch <- true
	}
	cancel := make(chan bool)
	defer func() { close(cancel) }()
	go d.ListenChanges(cancel, listenerAction)
	select {
	case <-ch:
		return
	case <-time.After(5 * time.Second):
		t.Fatal("The snapshot should have been set")
	}
}

func createTempDir(t *testing.T) string {
	t.Helper()
	rootPath, err := ioutil.TempDir("", "configPath")
	if err != nil {
		t.Fatal(err)
	}
	return rootPath
}

func deleteTempDir(t *testing.T, dir string) {
	t.Helper()
	err := os.RemoveAll(dir)
	if err != nil {
		t.Fatal(err)
	}
}

func copyAndParseSinglePartFile(t *testing.T, dir string, name string, content string) *unstructured.Unstructured {
	t.Helper()
	parts := copyAndParseFile(t, dir, name, content)
	if len(parts) != 1 {
		t.Fatalf("unexpected number of parts: %d", len(parts))
	}
	return parts[0]
}

func copyFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	err := ioutil.WriteFile(filepath.Join(dir, name), []byte(content), 0600)
	if err != nil {
		t.Fatal(err)
	}
}

func copyAndParseFile(t *testing.T, dir string, name string, content string) []*unstructured.Unstructured {
	t.Helper()
	copyFile(t, dir, name, content)
	parts := strings.Split(content, "---\n")
	out := make([]*unstructured.Unstructured, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			u := &unstructured.Unstructured{}
			parseYaml(t, part, u)
			out = append(out, u)
		}
	}
	return out
}

func deleteFiles(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, name := range files {
		err := os.Remove(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
}

func newOrFail(t *testing.T, dir string) runtime.Source {
	t.Helper()
	s, err := fs.New(dir, kubeMeta.Types, cfg)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if s == nil {
		t.Fatal("expected non-nil source")
	}
	return s
}

func startOrFail(t *testing.T, s runtime.Source) chan resource.Event {
	t.Helper()
	g := NewGomegaWithT(t)

	ch := make(chan resource.Event, 1024)
	err := s.Start(events.ChannelHandler(ch))
	g.Expect(err).To(BeNil())
	return ch
}

func expectFullSync(t *testing.T, ch chan resource.Event) {
	t.Helper()
	g := NewGomegaWithT(t)
	actual := events.ExpectOne(t, ch)
	g.Expect(actual).To(Equal(resource.FullSyncEvent))
}

func parseYaml(t *testing.T, yamlContent string, out k8sRuntime.Object) {
	if _, _, err := deserializer.Decode([]byte(yamlContent), nil, out); err != nil {
		t.Fatal(err)
	}
}

func getID(obj metav1.Object, spec schema.ResourceSpec) resource.VersionedKey {
	return resource.VersionedKey{
		Key: resource.Key{
			Collection: spec.Target.Collection,
			FullName:   resource.FullNameFromNamespaceAndName(obj.GetNamespace(), obj.GetName()),
		},
		Version: resource.Version(obj.GetResourceVersion()),
	}
}

func unstructuredToEntry(t *testing.T, u *unstructured.Unstructured, spec schema.ResourceSpec) resource.Entry {
	t.Helper()
	// Convert the unstructured to a converter entry, which contains the spec proto.
	key := resource.FullNameFromNamespaceAndName(u.GetNamespace(), u.GetName())
	entries, err := spec.Converter(cfg, spec.Target, key, spec.Kind, u)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) == 0 {
		t.Fatalf("no entries created for event")
	}

	entry := entries[0]
	return resource.Entry{
		ID:       getID(u, spec),
		Item:     entry.Resource,
		Metadata: entry.Metadata,
	}
}

func serviceToEntry(t *testing.T, svc coreV1.Service, spec schema.ResourceSpec) resource.Entry {
	t.Helper()

	return resource.Entry{
		ID:   getID(&svc, spec),
		Item: &svc.Spec,
		Metadata: resource.Metadata{
			CreateTime:  svc.CreationTimestamp.Time,
			Labels:      svc.Labels,
			Annotations: svc.Annotations,
		},
	}
}
