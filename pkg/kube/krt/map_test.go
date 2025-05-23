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

package krt_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test/util/assert"
)

func TestMapCollection(t *testing.T) {
	opts := testOptions(t)
	c1 := kube.NewFakeClient()
	kpc1 := kclient.New[*corev1.Pod](c1)
	pc1 := clienttest.Wrap(t, kpc1)
	pods := krt.WrapClient[*corev1.Pod](kpc1, opts.WithName("Pods1")...)
	c1.RunAndWait(opts.Stop())
	SimplePods := krt.MapCollection(pods, func(p *corev1.Pod) SimplePod {
		return SimplePod{
			Named: Named{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
			Labeled: Labeled{
				Labels: p.Labels,
			},
			IP: p.Status.PodIP,
		}
	}, opts.WithName("SimplePods")...)
	tt := assert.NewTracker[string](t)
	SimplePods.Register(TrackerHandler[SimplePod](tt))
	// Add a pod and make sure we get the event for it in event handlers from the mapped collection
	pc1.Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "ns1",
			Labels:    map[string]string{"a": "b"},
		},
		Status: corev1.PodStatus{
			PodIP: "1.2.3.4",
		},
	})
	tt.WaitOrdered("add/ns1/pod1")
}

func TestMapCollectionWithIndex(t *testing.T) {
	opts := testOptions(t)
	c1 := kube.NewFakeClient()
	kpc1 := kclient.New[*corev1.Pod](c1)
	pc1 := clienttest.Wrap(t, kpc1)
	pods := krt.WrapClient[*corev1.Pod](kpc1, opts.WithName("Pods1")...)
	c1.RunAndWait(opts.Stop())
	simplePods := krt.MapCollection(pods, func(p *corev1.Pod) SimplePod {
		return SimplePod{
			Named: Named{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
			Labeled: Labeled{
				Labels: p.Labels,
			},
			IP: p.Status.PodIP,
		}
	}, opts.WithName("SimplePods")...)
	spIndex := krt.NewIndex[string, SimplePod](simplePods, "by ip", func(p SimplePod) []string {
		return []string{p.IP}
	})
	pod1 := pc1.Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "ns1",
			Labels:    map[string]string{"a": "b"},
		},
		Status: corev1.PodStatus{
			PodIP: "1.2.3.4",
		},
	})
	pod2 := pc1.Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "ns1",
			Labels:    map[string]string{"a": "b"},
		},
		Status: corev1.PodStatus{
			PodIP: "1.2.3.5",
		},
	})
	pod3 := pc1.Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod3",
			Namespace: "ns1",
			Labels:    map[string]string{"a": "b"},
		},
		Status: corev1.PodStatus{
			PodIP: "1.2.3.6",
		},
	})
	assert.EventuallyEqual(t, func() SimplePod {
		vals := spIndex.Lookup("1.2.3.4")
		if len(vals) == 0 {
			return SimplePod{}
		}
		return vals[0]
	}, SimplePod{
		Named: Named{
			Name:      pod1.Name,
			Namespace: pod1.Namespace,
		},
		Labeled: Labeled{
			Labels: pod1.Labels,
		},
		IP: pod1.Status.PodIP,
	})
	assert.EventuallyEqual(t, func() SimplePod {
		vals := spIndex.Lookup("1.2.3.5")
		if len(vals) == 0 {
			return SimplePod{}
		}
		return vals[0]
	}, SimplePod{
		Named: Named{
			Name:      pod2.Name,
			Namespace: pod2.Namespace,
		},
		Labeled: Labeled{
			Labels: pod2.Labels,
		},
		IP: pod2.Status.PodIP,
	})
	assert.EventuallyEqual(t, func() SimplePod {
		vals := spIndex.Lookup("1.2.3.6")
		if len(vals) == 0 {
			return SimplePod{}
		}
		return vals[0]
	}, SimplePod{
		Named: Named{
			Name:      pod3.Name,
			Namespace: pod3.Namespace,
		},
		Labeled: Labeled{
			Labels: pod3.Labels,
		},
		IP: pod3.Status.PodIP,
	})
}

func TestMapCollectionMetadata(t *testing.T) {
	tc := []struct {
		name              string
		metadata          krt.Metadata
		useParentMetadata bool
	}{
		{
			name:     "no metadata",
			metadata: nil,
		},
		{
			name:     "empty metadata",
			metadata: krt.Metadata{},
		},
		{
			name:     "non-empty metadata",
			metadata: krt.Metadata{"foo": "bar"},
		},
		{
			name:              "non-empty metadata with parent",
			metadata:          krt.Metadata{"foo": "bar"},
			useParentMetadata: true,
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			c := kube.NewFakeClient()
			var parentOptions []krt.CollectionOption
			if tt.useParentMetadata {
				parentOptions = append(parentOptions, krt.WithMetadata(tt.metadata))
			}
			pods := krt.NewInformer[*corev1.Pod](c, append(opts.WithName("Pods"), parentOptions...)...)
			var options []krt.CollectionOption
			if !tt.useParentMetadata {
				options = append(options, krt.WithMetadata(tt.metadata))
			}
			simplePods := krt.MapCollection(pods, func(p *corev1.Pod) SimplePod {
				return SimplePod{
					Named: Named{
						Name:      p.Name,
						Namespace: p.Namespace,
					},
					Labeled: Labeled{
						Labels: p.Labels,
					},
					IP: p.Status.PodIP,
				}
			}, append(opts.WithName("SimplePods"), options...)...)
			assert.Equal(t, simplePods.Metadata(), tt.metadata)
			c.RunAndWait(opts.Stop())
		})
	}
}

func TestNestedMapCollection(t *testing.T) {
	opts := testOptions(t)
	c := kube.NewFakeClient()

	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	simplePods := krt.MapCollection(pods, func(p *corev1.Pod) SimplePod {
		return SimplePod{
			Named: Named{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
			Labeled: Labeled{
				Labels: p.Labels,
			},
			IP: p.Status.PodIP,
		}
	})
	MultiPods := krt.NewStaticCollection(
		nil,
		[]krt.Collection[SimplePod]{simplePods},
		opts.WithName("MultiPods")...,
	)
	AllPods := krt.NestedJoinCollection(
		MultiPods,
		opts.WithName("AllPods")...,
	)
	c.RunAndWait(opts.Stop())
	assert.EventuallyEqual(t, func() bool {
		return AllPods.WaitUntilSynced(opts.Stop())
	}, true)

	SimplePods := krt.MapCollection(AllPods, func(p SimplePod) SimpleSizedPod {
		return SimpleSizedPod{
			SimplePod: p,
			Size:      "50",
		}
	}, opts.WithName("SimplePods")...)

	tt := assert.NewTracker[string](t)
	SimplePods.Register(TrackerHandler[SimpleSizedPod](tt))

	c2 := kube.NewFakeClient()
	pods2 := krt.NewInformer[*corev1.Pod](c2, opts.WithName("Pods2")...)
	simplePods2 := krt.MapCollection(pods2, func(p *corev1.Pod) SimplePod {
		return SimplePod{
			Named: Named{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
			Labeled: Labeled{
				Labels: p.Labels,
			},
			IP: p.Status.PodIP,
		}
	}, opts.WithName("SimplePods2")...)
	c2.RunAndWait(opts.Stop())
	MultiPods.UpdateObject(simplePods2)
	tt.Empty() // Shouldn't be any events since no pods were added
	MultiPods.DeleteObject(krt.GetKey(pods2))
	tt.Empty() // Shouldn't be any events since no pods were added
}
