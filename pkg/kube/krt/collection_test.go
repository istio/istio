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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istioclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

// KrtOptions is a small wrapper around KRT options to make it easy to provide a common set of options to all collections
// without excessive duplication.
type KrtOptions struct {
	stop chan struct{}
}

func (k KrtOptions) WithName(n string) []krt.CollectionOption {
	return []krt.CollectionOption{krt.WithDebugging(krt.GlobalDebugHandler), krt.WithStop(k.stop), krt.WithName(n)}
}

type SimplePod struct {
	Named
	Labeled
	IP string
}

func SimplePodCollection(pods krt.Collection[*corev1.Pod], opts KrtOptions) krt.Collection[SimplePod] {
	return krt.NewCollection(pods, func(ctx krt.HandlerContext, i *corev1.Pod) *SimplePod {
		if i.Status.PodIP == "" {
			return nil
		}
		return &SimplePod{
			Named:   NewNamed(i),
			Labeled: NewLabeled(i.Labels),
			IP:      i.Status.PodIP,
		}
	}, opts.WithName("SimplePods")...)
}

type SizedPod struct {
	Named
	Size string
}

func SizedPodCollection(pods krt.Collection[*corev1.Pod], opts KrtOptions) krt.Collection[SizedPod] {
	return krt.NewCollection(pods, func(ctx krt.HandlerContext, i *corev1.Pod) *SizedPod {
		s, f := i.Labels["size"]
		if !f {
			return nil
		}
		return &SizedPod{
			Named: NewNamed(i),
			Size:  s,
		}
	}, opts.WithName("SizedPods")...)
}

func NewNamed(n config.Namer) Named {
	return Named{
		Namespace: n.GetNamespace(),
		Name:      n.GetName(),
	}
}

type Named struct {
	Namespace string
	Name      string
}

func (s Named) ResourceName() string {
	return s.Namespace + "/" + s.Name
}

func NewLabeled(n map[string]string) Labeled {
	return Labeled{n}
}

type Labeled struct {
	Labels map[string]string
}

func (l Labeled) GetLabels() map[string]string {
	return l.Labels
}

type SimpleService struct {
	Named
	Selector map[string]string
}

func SimpleServiceCollection(services krt.Collection[*corev1.Service], opts KrtOptions) krt.Collection[SimpleService] {
	return krt.NewCollection(services, func(ctx krt.HandlerContext, i *corev1.Service) *SimpleService {
		return &SimpleService{
			Named:    NewNamed(i),
			Selector: i.Spec.Selector,
		}
	}, opts.WithName("SimpleService")...)
}

func SimpleServiceCollectionFromEntries(entries krt.Collection[*istioclient.ServiceEntry], opts KrtOptions) krt.Collection[SimpleService] {
	return krt.NewCollection(entries, func(ctx krt.HandlerContext, i *istioclient.ServiceEntry) *SimpleService {
		l := i.Spec.WorkloadSelector.GetLabels()
		if l == nil {
			return nil
		}
		return &SimpleService{
			Named:    NewNamed(i),
			Selector: l,
		}
	}, opts.WithName("SimpleService")...)
}

type SimpleEndpoint struct {
	Pod       string
	Service   string
	Namespace string
	IP        string
}

func (s SimpleEndpoint) ResourceName() string {
	return slices.Join("/", s.Namespace+"/"+s.Service+"/"+s.Pod)
}

func SimpleEndpointsCollection(pods krt.Collection[SimplePod], services krt.Collection[SimpleService], opts KrtOptions) krt.Collection[SimpleEndpoint] {
	return krt.NewManyCollection[SimpleService, SimpleEndpoint](services, func(ctx krt.HandlerContext, svc SimpleService) []SimpleEndpoint {
		pods := krt.Fetch(ctx, pods, krt.FilterLabel(svc.Selector))
		return slices.Map(pods, func(pod SimplePod) SimpleEndpoint {
			return SimpleEndpoint{
				Pod:       pod.Name,
				Service:   svc.Name,
				Namespace: svc.Namespace,
				IP:        pod.IP,
			}
		})
	}, opts.WithName("SimpleEndpoints")...)
}

func init() {
	log.FindScope("krt").SetOutputLevel(log.DebugLevel)
}

func TestCollectionSimple(t *testing.T) {
	stop := test.NewStop(t)
	opts := KrtOptions{stop}
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	pc := clienttest.Wrap(t, kpc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)

	assert.Equal(t, fetcherSorted(SimplePods)(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
	}
	pc.Create(pod)
	assert.Equal(t, fetcherSorted(SimplePods)(), nil)

	pod.Status = corev1.PodStatus{PodIP: "1.2.3.4"}
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimplePods), []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.4"}})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimplePods), []SimplePod{{NewNamed(pod), Labeled{}, "1.2.3.5"}})

	// check we get updates if we add a handler later
	tt := assert.NewTracker[string](t)
	SimplePods.Register(TrackerHandler[SimplePod](tt))
	tt.WaitUnordered("add/namespace/name")

	pc.Delete(pod.Name, pod.Namespace)
	assert.EventuallyEqual(t, fetcherSorted(SimplePods), nil)
	tt.WaitUnordered("delete/namespace/name")
}

func TestCollectionInitialState(t *testing.T) {
	stop := test.NewStop(t)
	opts := KrtOptions{stop}
	c := kube.NewFakeClient(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "namespace",
				Labels:    map[string]string{"app": "foo"},
			},
			Status: corev1.PodStatus{PodIP: "1.2.3.4"},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc",
				Namespace: "namespace",
			},
			Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
		},
	)
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	services := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	c.RunAndWait(stop)
	SimplePods := SimplePodCollection(pods, opts)
	SimpleServices := SimpleServiceCollection(services, opts)
	SimpleEndpoints := SimpleEndpointsCollection(SimplePods, SimpleServices, opts)
	assert.Equal(t, SimpleEndpoints.Synced().WaitUntilSynced(stop), true)
	// Assert Equal -- not EventuallyEqual -- to ensure our WaitForCacheSync is proper
	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), []SimpleEndpoint{{"pod", "svc", "namespace", "1.2.3.4"}})
}

func TestCollectionMerged(t *testing.T) {
	stop := test.NewStop(t)
	opts := KrtOptions{stop}
	c := kube.NewFakeClient()
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	services := krt.NewInformer[*corev1.Service](c, opts.WithName("Services")...)
	c.RunAndWait(stop)
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	sc := clienttest.Wrap(t, kclient.New[*corev1.Service](c))
	SimplePods := SimplePodCollection(pods, opts)
	SimpleServices := SimpleServiceCollection(services, opts)
	SimpleEndpoints := SimpleEndpointsCollection(SimplePods, SimpleServices, opts)

	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
	}
	pc.Create(pod)
	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), nil)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "namespace",
		},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "foo"}},
	}
	sc.Create(svc)
	assert.Equal(t, fetcherSorted(SimpleEndpoints)(), nil)

	pod.Status = corev1.PodStatus{PodIP: "1.2.3.4"}
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.4"}})

	pod.Status.PodIP = "1.2.3.5"
	pc.UpdateStatus(pod)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), []SimpleEndpoint{{pod.Name, svc.Name, pod.Namespace, "1.2.3.5"}})

	pc.Delete(pod.Name, pod.Namespace)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), nil)

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name2",
			Namespace: "namespace",
			Labels:    map[string]string{"app": "foo"},
		},
		Status: corev1.PodStatus{PodIP: "2.3.4.5"},
	}
	pc.CreateOrUpdateStatus(pod)
	pc.CreateOrUpdateStatus(pod2)
	assert.EventuallyEqual(t, fetcherSorted(SimpleEndpoints), []SimpleEndpoint{
		{pod2.Name, svc.Name, pod2.Namespace, pod2.Status.PodIP},
		{pod.Name, svc.Name, pod.Namespace, pod.Status.PodIP},
	})
}

type PodSizeCount struct {
	Named
	MatchingSizes int
}

func TestCollectionCycle(t *testing.T) {
	stop := test.NewStop(t)
	opts := KrtOptions{stop}
	c := kube.NewFakeClient()
	pods := krt.NewInformer[*corev1.Pod](c, opts.WithName("Pods")...)
	c.RunAndWait(stop)
	pc := clienttest.Wrap(t, kclient.New[*corev1.Pod](c))
	SimplePods := SimplePodCollection(pods, opts)
	SizedPods := SizedPodCollection(pods, opts)
	Thingys := krt.NewCollection[SimplePod, PodSizeCount](SimplePods, func(ctx krt.HandlerContext, pd SimplePod) *PodSizeCount {
		if _, f := pd.Labels["want-size"]; !f {
			return nil
		}
		matches := krt.Fetch(ctx, SizedPods, krt.FilterGeneric(func(a any) bool {
			return a.(SizedPod).Size == pd.Labels["want-size"]
		}))
		return &PodSizeCount{
			Named:         pd.Named,
			MatchingSizes: len(matches),
		}
	}, opts.WithName("Thingys")...)
	tt := assert.NewTracker[string](t)
	Thingys.RegisterBatch(BatchedTrackerHandler[PodSizeCount](tt), true)

	assert.Equal(t, fetcherSorted(Thingys)(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
			Labels:    map[string]string{"want-size": "large"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.4"},
	}
	pc.CreateOrUpdateStatus(pod)
	tt.WaitOrdered("add/namespace/name")
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 0,
	}})

	largePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-large",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "large"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.5"},
	}
	pc.CreateOrUpdateStatus(largePod)
	tt.WaitOrdered("update/namespace/name")
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 1,
	}})

	smallPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-small",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "small"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.6"},
	}
	pc.CreateOrUpdateStatus(smallPod)
	pc.CreateOrUpdateStatus(largePod)
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 1,
	}})
	tt.Empty()

	largePod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-large2",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "large"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.7"},
	}
	pc.CreateOrUpdateStatus(largePod2)
	tt.WaitOrdered("update/namespace/name")
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 2,
	}})

	dual := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name-dual",
			Namespace: "namespace",
			Labels:    map[string]string{"size": "large", "want-size": "small"},
		},
		Status: corev1.PodStatus{PodIP: "1.2.3.8"},
	}
	pc.CreateOrUpdateStatus(dual)
	tt.WaitUnordered("update/namespace/name", "add/namespace/name-dual")
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{
		{
			Named:         NewNamed(pod),
			MatchingSizes: 3,
		},
		{
			Named:         NewNamed(dual),
			MatchingSizes: 1,
		},
	})

	largePod2.Labels["size"] = "small"
	pc.CreateOrUpdateStatus(largePod2)
	tt.WaitCompare(CompareUnordered("update/namespace/name-dual", "update/namespace/name"))
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{
		{
			Named:         NewNamed(pod),
			MatchingSizes: 2,
		},
		{
			Named:         NewNamed(dual),
			MatchingSizes: 2,
		},
	})

	pc.Delete(dual.Name, dual.Namespace)
	tt.WaitUnordered("update/namespace/name", "delete/namespace/name-dual")
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 1,
	}})

	pc.Delete(largePod.Name, largePod.Namespace)
	tt.WaitOrdered("update/namespace/name")
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{{
		Named:         NewNamed(pod),
		MatchingSizes: 0,
	}})

	pc.Delete(pod.Name, pod.Namespace)
	tt.WaitOrdered("delete/namespace/name")
	assert.Equal(t, fetcherSorted(Thingys)(), []PodSizeCount{})
}

func CompareUnordered(wants ...string) func(s string) bool {
	want := sets.New(wants...)
	return func(s string) bool {
		got := sets.New(strings.Split(s, ",")...)
		return want.Equals(got)
	}
}

func fetcherSorted[T krt.ResourceNamer](c krt.Collection[T]) func() []T {
	return func() []T {
		return slices.SortBy(c.List(), func(t T) string {
			return t.ResourceName()
		})
	}
}

func TestCollectionMultipleFetch(t *testing.T) {
	stop := test.NewStop(t)
	opts := KrtOptions{stop}
	type Result struct {
		Named
		Configs []string
	}
	c := kube.NewFakeClient()
	kpc := kclient.New[*corev1.Pod](c)
	kcc := kclient.New[*corev1.ConfigMap](c)
	pc := clienttest.Wrap(t, kpc)
	cc := clienttest.Wrap(t, kcc)
	pods := krt.WrapClient[*corev1.Pod](kpc, opts.WithName("Pods")...)
	configMaps := krt.WrapClient[*corev1.ConfigMap](kcc, opts.WithName("ConfigMaps")...)
	c.RunAndWait(stop)

	lblFoo := map[string]string{"app": "foo"}
	lblBar := map[string]string{"app": "bar"}
	// Setup a simple collection that fetches the same dependency twice
	Results := krt.NewCollection(pods, func(ctx krt.HandlerContext, i *corev1.Pod) *Result {
		foos := krt.Fetch(ctx, configMaps, krt.FilterLabel(lblFoo))
		bars := krt.Fetch(ctx, configMaps, krt.FilterLabel(lblBar))
		names := slices.Map(foos, func(f *corev1.ConfigMap) string { return f.Name })
		names = append(names, slices.Map(bars, func(f *corev1.ConfigMap) string { return f.Name })...)
		names = slices.Sort(names)
		return &Result{
			Named:   NewNamed(i),
			Configs: slices.Sort(names),
		}
	}, opts.WithName("Results")...)

	assert.Equal(t, fetcherSorted(Results)(), nil)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: "namespace",
		},
	}
	pc.Create(pod)
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), nil}})

	cc.Create(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "foo1", Labels: lblFoo}})
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), []string{"foo1"}}})

	cc.Create(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "switch", Labels: lblFoo}})
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), []string{"foo1", "switch"}}})

	cc.Create(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "bar1", Labels: lblBar}})
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), []string{"bar1", "foo1", "switch"}}})

	cc.Update(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "switch", Labels: lblBar}})
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), []string{"bar1", "foo1", "switch"}}})

	cc.Update(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "switch", Labels: nil}})
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), []string{"bar1", "foo1"}}})

	cc.Delete("bar1", "")
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), []string{"foo1"}}})

	cc.Delete("foo1", "")
	assert.EventuallyEqual(t, fetcherSorted(Results), []Result{{NewNamed(pod), nil}})
}
