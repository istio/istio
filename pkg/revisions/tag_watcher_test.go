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
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestTagWatcher(t *testing.T) {
	c := kube.NewFakeClient()
	tw := NewTagWatcher(c, "revision", "istio-system").(*tagWatcher)
	whs := clienttest.Wrap(t, tw.webhooks)
	svcs := clienttest.Wrap(t, tw.services)
	stop := test.NewStop(t)
	c.RunAndWait(stop)
	track := assert.NewTracker[string](t)
	tw.AddHandler(func(s sets.String) {
		track.Record(strings.Join(sets.SortedList(s), ","))
	})
	go tw.Run(stop)
	kube.WaitForCacheSync("test", stop, tw.HasSynced)
	track.WaitOrdered("revision")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision"))

	whs.Create(makeRevision("revision"))
	whs.Create(makeTag("revision", "tag-foo"))
	track.WaitOrdered("revision,tag-foo")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision", "tag-foo"))

	whs.Create(makeTag("revision", "tag-bar"))
	track.WaitOrdered("revision,tag-bar,tag-foo")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision", "tag-foo", "tag-bar"))

	whs.Update(makeTag("not-revision", "tag-bar"))
	track.WaitOrdered("revision,tag-foo")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision", "tag-foo"))

	whs.Delete(makeTag("revision", "tag-foo").Name, "")
	track.WaitOrdered("revision")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision"))

	whs.Create(makeTag("revision", "tag-foo"))
	track.WaitOrdered("revision,tag-foo")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision", "tag-foo"))

	// TagWatcher should get all tag names from all services and mutatingwebhooks using the same revision
	whs.Delete(makeTag("revision", "tag-foo").Name, "")
	track.WaitOrdered("revision")
	svcs.Create(makeServiceTag("revision", "tag-svc-foo"))
	track.WaitOrdered("revision,tag-svc-foo")
	whs.Create(makeTag("revision", "tag-mwc-foo"))
	track.WaitOrdered("revision,tag-mwc-foo,tag-svc-foo")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision", "tag-mwc-foo", "tag-svc-foo"))

	// If a svc and a mwc have the same tag but different revisions, svc should have more priority
	svcs.Create(makeServiceTag("revision", "shared-tag"))
	track.WaitOrdered("revision,shared-tag,tag-mwc-foo,tag-svc-foo")
	whs.Create(makeTag("not-revision", "shared-tag"))
	track.WaitOrdered("revision,shared-tag,tag-mwc-foo,tag-svc-foo")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision", "tag-mwc-foo", "tag-svc-foo", "shared-tag"))

	// If a svc and mwc have same tag and revisions, there should not be duplicates
	whs.Update(makeTag("revision", "shared-tag"))
	track.WaitOrdered("revision,shared-tag,tag-mwc-foo,tag-svc-foo")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision", "tag-mwc-foo", "tag-svc-foo", "shared-tag"))
}

func TestTagWatcherServicePrecedence(t *testing.T) {
	for _, tagName := range []string{"shared-tag", "revision-a"} {
		t.Run(tagName, func(t *testing.T) {
			c := kube.NewFakeClient(&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "tagged",
					Labels: map[string]string{
						label.IoIstioRev.Name: tagName,
					},
				},
			})
			watchers := []*tagWatcher{
				NewTagWatcher(c, "revision-a", "istio-system").(*tagWatcher),
				NewTagWatcher(c, "revision-b", "istio-system").(*tagWatcher),
			}
			tracks := make([]*assert.Tracker[string], 0, len(watchers))
			for _, tw := range watchers {
				track := assert.NewTracker[string](t)
				tw.AddHandler(func(tags sets.String) {
					track.Record(strings.Join(sets.SortedList(tags), ","))
				})
				tracks = append(tracks, track)
			}
			stop := test.NewStop(t)
			c.RunAndWait(stop)
			for _, tw := range watchers {
				go tw.Run(stop)
				kube.WaitForCacheSync("test", stop, tw.HasSynced)
			}
			checkOwner := func(revision string) {
				t.Helper()
				for i, tw := range watchers {
					// A tag cannot remove the watcher's own revision name.
					ownsTag := tw.revision == revision || tw.revision == tagName
					want := sets.New(tw.revision)
					if ownsTag {
						want.Insert(tagName)
					}
					tracks[i].WaitOrdered(strings.Join(sets.SortedList(want), ","))
					assert.Equal(t, tw.GetMyTags(), want)
					assert.Equal(t, tw.IsMine(metav1.ObjectMeta{
						Labels: map[string]string{label.IoIstioRev.Name: tagName},
					}), ownsTag, "object revision label", tw.revision)
					assert.Equal(t, tw.IsMine(metav1.ObjectMeta{Namespace: "tagged"}), ownsTag,
						"namespace revision label", tw.revision)
				}
			}
			checkOwner("")
			whs := clienttest.Wrap(t, watchers[0].webhooks)
			svcs := clienttest.Wrap(t, watchers[0].services)

			// A tag represented by only a webhook belongs to its revision.
			whs.Create(makeTag("revision-a", tagName))
			checkOwner("revision-a")

			// A Service pointing elsewhere must override the webhook for both watchers.
			svcs.Create(makeServiceTag("revision-b", tagName))
			checkOwner("revision-b")

			// Moving the Service transfers ownership, including when both resources agree.
			svcs.Update(makeServiceTag("revision-a", tagName))
			checkOwner("revision-a")
			svcs.Update(makeServiceTag("revision-b", tagName))
			checkOwner("revision-b")

			// Removing the Service restores the existing webhook's ownership.
			svcs.Delete(tagName, "istio-system")
			checkOwner("revision-a")
		})
	}
}

func makeTag(revision string, tg string) *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: tg,
			Labels: map[string]string{
				label.IoIstioRev.Name: revision,
				label.IoIstioTag.Name: tg,
			},
		},
	}
}

func makeServiceTag(revision string, tg string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: tg,
			Labels: map[string]string{
				label.IoIstioRev.Name: revision,
				label.IoIstioTag.Name: tg,
			},
			Namespace: "istio-system",
		},
	}
}

func makeRevision(revision string) *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: revision,
			Labels: map[string]string{
				label.IoIstioRev.Name: revision,
			},
		},
	}
}
