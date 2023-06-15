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
	tw := NewTagWatcher(c, "revision").(*tagWatcher)
	whs := clienttest.Wrap(t, tw.webhooks)
	stop := test.NewStop(t)
	c.RunAndWait(stop)
	track := assert.NewTracker[string](t)
	tw.AddHandler(func(s sets.Set[string]) {
		track.Record(strings.Join(sets.SortedList(s), ","))
	})
	go tw.Run(stop)
	kube.WaitForCacheSync("test", stop, tw.HasSynced)
	track.WaitOrdered("revision")
	assert.Equal(t, tw.GetMyTags(), sets.New("revision"))

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
}

func makeTag(revision string, tg string) *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: tg,
			Labels: map[string]string{
				label.IoIstioRev.Name: revision,
				"istio.io/tag":        tg,
			},
		},
	}
}
