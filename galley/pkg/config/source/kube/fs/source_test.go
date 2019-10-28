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
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/pkg/appsignals"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/source/kube/fs"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
)

func TestNew(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	_ = newOrFail(t, dir)
}

func TestInvalidDirShouldSucceed(t *testing.T) {
	s := newOrFail(t, "somebaddir")

	acc := startOrFail(t, s)
	defer s.Stop()

	fixtures.ExpectFilter(t, acc, fixtures.NoFullSync)
}

func TestInitialFile(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	copyFile(t, dir, "foo.yaml", data.YamlN1I1V1)

	// Start the source.
	s := newOrFail(t, dir)
	acc := startOrFail(t, s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.AddFor(data.Collection1, data.EntryN1I1V1)))

	acc.Clear()

	deleteFiles(t, dir, "foo.yaml")
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(data.Collection1, data.EntryN1I1V1)))
}

func TestInitialFileWatcherEnabled(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	copyFile(t, dir, "foo.yaml", data.YamlN1I1V1)

	// Start the source.
	s := newWithWatcherEnabledOrFail(t, dir)
	acc := startOrFail(t, s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.AddFor(data.Collection1, data.EntryN1I1V1)))

	acc.Clear()

	deleteFiles(t, dir, "foo.yaml")

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(data.Collection1, data.EntryN1I1V1)))
}

func TestAddDeleteMultipleTimes(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newOrFail(t, dir)
	acc := startOrFail(t, s)
	defer s.Stop()

	copyFile(t, dir, "foo.yaml", data.YamlN1I1V1)
	appsignals.Notify("test", syscall.SIGUSR1)
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.AddFor(data.Collection1, data.EntryN1I1V1)))

	acc.Clear()
	deleteFiles(t, dir, "foo.yaml")
	appsignals.Notify("test", syscall.SIGUSR1)
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(data.Collection1, data.EntryN1I1V1)))

	acc.Clear()
	copyFile(t, dir, "foo.yaml", data.YamlN1I1V1)
	appsignals.Notify("test", syscall.SIGUSR1)
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.AddFor(data.Collection1, withVersion(data.EntryN1I1V1, "v2"))))

	acc.Clear()
	deleteFiles(t, dir, "foo.yaml")
	appsignals.Notify("test", syscall.SIGUSR1)
	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(data.Collection1, withVersion(data.EntryN1I1V1, "v2"))))
}

func TestAddDeleteMultipleTimes1(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newOrFail(t, dir)
	acc := startOrFail(t, s)
	defer s.Stop()

	copyFile(t, dir, "foo.yaml", data.YamlN1I1V1)
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.AddFor(data.Collection1, data.EntryN1I1V1)))

	acc.Clear()
	deleteFiles(t, dir, "foo.yaml")
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(data.Collection1, data.EntryN1I1V1)))
}

func TestAddUpdateDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newOrFail(t, dir)
	acc := startOrFail(t, s)
	defer s.Stop()

	copyFile(t, dir, "foo.yaml", data.YamlN1I1V1)
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.AddFor(data.Collection1, data.EntryN1I1V1)))

	acc.Clear()
	copyFile(t, dir, "foo.yaml", data.YamlN1I1V2)
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.UpdateFor(data.Collection1, withVersion(data.EntryN1I1V2, "v2"))))

	acc.Clear()
	copyFile(t, dir, "foo.yaml", "")
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(data.Collection1, withVersion(data.EntryN1I1V2, "v2"))))
}

func TestAddUpdateDeleteWithWatcherEnabled(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newWithWatcherEnabledOrFail(t, dir)
	acc := startOrFail(t, s)
	defer s.Stop()

	copyFile(t, dir, "foo.yaml", data.YamlN1I1V1)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.AddFor(data.Collection1, data.EntryN1I1V1)))

	acc.Clear()
	copyFile(t, dir, "foo.yaml", data.YamlN1I1V2)

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.UpdateFor(data.Collection1, withVersion(data.EntryN1I1V2, "v2"))))

	acc.Clear()
	copyFile(t, dir, "foo.yaml", "")

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.DeleteForResource(data.Collection1, withVersion(data.EntryN1I1V2, "v2"))))
}

func TestAddUpdateDelete_K8sResources(t *testing.T) {
	g := NewGomegaWithT(t)

	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newWithMetadataOrFail(t, dir, k8smeta.MustGet())
	acc := startOrFail(t, s)
	defer s.Stop()

	g.Eventually(acc.EventsWithoutOrigins).Should(ConsistOf(
		event.FullSyncFor(k8smeta.K8SCoreV1Endpoints),
		event.FullSyncFor(k8smeta.K8SExtensionsV1Beta1Ingresses),
		event.FullSyncFor(k8smeta.K8SCoreV1Namespaces),
		event.FullSyncFor(k8smeta.K8SCoreV1Nodes),
		event.FullSyncFor(k8smeta.K8SCoreV1Pods),
		event.FullSyncFor(k8smeta.K8SAppsV1Deployments),
		event.FullSyncFor(k8smeta.K8SCoreV1Services)))

	acc.Clear()
	copyFile(t, dir, "bar.yaml", data.GetService())
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(HaveLen(1))
	g.Expect(acc.EventsWithoutOrigins()[0].Source).To(Equal(k8smeta.K8SCoreV1Services))
	g.Expect(acc.EventsWithoutOrigins()[0].Kind).To(Equal(event.Added))
	g.Expect(acc.EventsWithoutOrigins()[0].Entry.Metadata.Name).To(Equal(resource.NewName("kube-system", "kube-dns")))

	acc.Clear()
	deleteFiles(t, dir, "bar.yaml")
	appsignals.Notify("test", syscall.SIGUSR1)

	g.Eventually(acc.EventsWithoutOrigins).Should(HaveLen(1))
	g.Expect(acc.EventsWithoutOrigins()[0].Source).To(Equal(k8smeta.K8SCoreV1Services))
	g.Expect(acc.EventsWithoutOrigins()[0].Kind).To(Equal(event.Deleted))
	g.Expect(acc.EventsWithoutOrigins()[0].Entry.Metadata.Name).To(Equal(resource.NewName("kube-system", "kube-dns")))
}

func TestMultiStart(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newOrFail(t, dir)
	defer s.Stop()

	s.Start()

	s.Start()
	// No crash
}

func TestMultiStop(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newOrFail(t, dir)
	s.Start()

	s.Stop()
	s.Stop() // Ensure that it does not crash
}

func TestStartStopStart(t *testing.T) {
	dir := createTempDir(t)
	defer deleteTempDir(t, dir)

	s := newOrFail(t, dir)
	defer s.Stop()

	s.Start()

	s.Stop()

	s.Start()

	s.Stop()

	s.Start()

	s.Stop()
}

func createTempDir(t *testing.T) string {
	t.Helper()
	rootPath, err := ioutil.TempDir("", "configPath")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Root dir: %v", rootPath)
	return rootPath
}

func deleteTempDir(t *testing.T, dir string) {
	t.Helper()
	err := os.RemoveAll(dir)
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	err := ioutil.WriteFile(filepath.Join(dir, name), []byte(content), 0600)
	if err != nil {
		t.Fatal(err)
	}
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

func newOrFail(t *testing.T, dir string) event.Source {
	t.Helper()
	return newWithMetadataOrFail(t, dir, basicmeta.MustGet())
}

func newWithMetadataOrFail(t *testing.T, dir string, m *schema.Metadata) event.Source {
	t.Helper()
	s, err := fs.New(dir, m.KubeSource().Resources(), false)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if s == nil {
		t.Fatal("expected non-nil source")
	}
	return s
}

func newWithWatcherEnabledOrFail(t *testing.T, dir string) event.Source {
	t.Helper()
	s, err := fs.New(dir, basicmeta.MustGet().KubeSource().Resources(), true)
	if err != nil {
		t.Fatalf("Unexpected error found: %v", err)
	}

	if s == nil {
		t.Fatal("expected non-nil source")
	}
	return s
}

func startOrFail(t *testing.T, s event.Source) *fixtures.Accumulator {
	t.Helper()

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)
	s.Start()

	return acc
}

func withVersion(r *resource.Entry, v string) *resource.Entry { // nolint:unparam
	r = r.Clone()
	r.Metadata.Version = resource.Version(v)
	return r
}
