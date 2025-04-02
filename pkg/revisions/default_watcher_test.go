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

package revisions

import (
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func webhook(revision string) *v1.MutatingWebhookConfiguration {
	return &v1.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultTagWebhookName,
			Labels: map[string]string{
				label.IoIstioRev.Name: revision,
			},
		},
	}
}

func expectRevision(t test.Failer, watcher DefaultWatcher, expected string) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		got := watcher.GetDefault()
		if got != expected {
			return fmt.Errorf("wanted default revision %q, got %q", expected, got)
		}
		return nil
	}, retry.Timeout(time.Second*10), retry.BackoffDelay(time.Millisecond*10))
}

func expectRevisionChan(t test.Failer, revisionChan chan string, expected string) {
	select {
	case rev := <-revisionChan:
		if rev != expected {
			t.Fatalf("expected revision %q to be produced on chan, got %q", expected, rev)
		}
	case <-time.After(time.Second * 5):
		t.Fatal("timed out waiting for value on default revision chan")
	}
}

func TestNoDefaultRevision(t *testing.T) {
	stop := make(chan struct{})
	client := kube.NewFakeClient()
	w := NewDefaultWatcher(client, "default")
	client.RunAndWait(stop)
	go w.Run(stop)
	// if have no default tag for some reason, should return ""
	expectRevision(t, w, "")
	close(stop)
}

func TestDefaultRevisionChanges(t *testing.T) {
	stop := test.NewStop(t)
	client := kube.NewFakeClient()
	w := NewDefaultWatcher(client, "default").(*defaultWatcher)
	client.RunAndWait(stop)
	go w.Run(stop)
	whc := clienttest.Wrap(t, w.webhooks)
	expectRevision(t, w, "")
	// change default to "red"
	whc.CreateOrUpdate(webhook("red"))
	expectRevision(t, w, "red")

	// change default to "green"
	whc.CreateOrUpdate(webhook("green"))
	expectRevision(t, w, "green")

	// remove default
	whc.Delete(defaultTagWebhookName, "")
	expectRevision(t, w, "")
}

func TestHandlers(t *testing.T) {
	stop := test.NewStop(t)
	client := kube.NewFakeClient()
	w := NewDefaultWatcher(client, "default").(*defaultWatcher)
	client.RunAndWait(stop)
	go w.Run(stop)
	whc := clienttest.Wrap(t, w.webhooks)
	expectRevision(t, w, "")

	// add a handler to watch default revision changes, ensure it's triggered
	newDefaultChan := make(chan string)
	handler := func(revision string) {
		newDefaultChan <- revision
	}
	w.AddHandler(handler)
	whc.CreateOrUpdate(webhook("green"))
	expectRevisionChan(t, newDefaultChan, "green")
}
