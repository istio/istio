// Copyright 2016 The prometheus-operator Authors
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

package listwatch

import (
	"fmt"
	"strings"
	"sync"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
)

// NewUnprivilegedNamespaceListWatchFromClient mimics
// cache.NewListWatchFromClient.
// It allows for the creation of a cache.ListWatch for namespaces from a client
// that does not have `List` privileges. If the slice of namespaces contains
// only v1.NamespaceAll, then this func assumes that the client has List and
// Watch privileges and returns a regular cache.ListWatch, since there is no
// other way to get all namespaces.
func NewUnprivilegedNamespaceListWatchFromClient(c cache.Getter, namespaces []string, fieldSelector fields.Selector) *cache.ListWatch {
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = fieldSelector.String()
	}
	return NewFilteredUnprivilegedNamespaceListWatchFromClient(c, namespaces, optionsModifier)
}

// NewFilteredUnprivilegedNamespaceListWatchFromClient mimics
// cache.NewUnprivilegedNamespaceListWatchFromClient.
// It allows for the creation of a cache.ListWatch for namespaces from a client
// that does not have `List` privileges. If the slice of namespaces contains
// only v1.NamespaceAll, then this func assumes that the client has List and
// Watch privileges and returns a regular cache.ListWatch, since there is no
// other way to get all namespaces.
func NewFilteredUnprivilegedNamespaceListWatchFromClient(c cache.Getter, namespaces []string, optionsModifier func(options *metav1.ListOptions)) *cache.ListWatch {
	// If the only namespace given is `v1.NamespaceAll`, then this
	// cache.ListWatch must be privileged. In this case, return a regular
	// cache.ListWatch.
	if IsAllNamespaces(namespaces) {
		return cache.NewFilteredListWatchFromClient(c, "namespaces", metav1.NamespaceAll, optionsModifier)
	}
	listFunc := func(options metav1.ListOptions) (runtime.Object, error) {
		optionsModifier(&options)
		list := &v1.NamespaceList{}
		for _, name := range namespaces {
			result := &v1.Namespace{}
			err := c.Get().
				Resource("namespaces").
				Name(name).
				VersionedParams(&options, scheme.ParameterCodec).
				Do().
				Into(result)
			if err != nil {
				return nil, err
			}
			list.Items = append(list.Items, *result)
		}
		return list, nil
	}
	watchFunc := func(_ metav1.ListOptions) (watch.Interface, error) {
		// Since the client does not have Watch privileges, do not
		// actually watch anything. Use a watch.FakeWatcher here to
		// implement watch.Interface but not send any events.
		return watch.NewFake(), nil
	}
	return &cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}

// MultiNamespaceListerWatcher takes a list of namespaces and a
// cache.ListerWatcher generator func and returns a single cache.ListerWatcher
// capable of operating on multiple namespaces.
func MultiNamespaceListerWatcher(namespaces []string, f func(string) cache.ListerWatcher) cache.ListerWatcher {
	// If there is only one namespace then there is no need to create a
	// proxy.
	if len(namespaces) == 1 {
		return f(namespaces[0])
	}
	var lws []cache.ListerWatcher
	for _, n := range namespaces {
		lws = append(lws, f(n))
	}
	return multiListerWatcher(lws)
}

// multiListerWatcher abstracts several cache.ListerWatchers, allowing them
// to be treated as a single cache.ListerWatcher.
type multiListerWatcher []cache.ListerWatcher

// List implements the ListerWatcher interface.
// It combines the output of the List method of every ListerWatcher into
// a single result.
func (mlw multiListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	l := metav1.List{}
	var resourceVersions []string
	for _, lw := range mlw {
		list, err := lw.List(options)
		if err != nil {
			return nil, err
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			return nil, err
		}
		metaObj, err := meta.ListAccessor(list)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			l.Items = append(l.Items, runtime.RawExtension{Object: item.DeepCopyObject()})
		}
		resourceVersions = append(resourceVersions, metaObj.GetResourceVersion())
	}
	// Combine the resource versions so that the composite Watch method can
	// distribute appropriate versions to each underlying Watch func.
	l.ListMeta.ResourceVersion = strings.Join(resourceVersions, "/")
	return &l, nil
}

// Watch implements the ListerWatcher interface.
// It returns a watch.Interface that combines the output from the
// watch.Interface of every cache.ListerWatcher into a single result chan.
func (mlw multiListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	resourceVersions := make([]string, len(mlw))
	// Allow resource versions to be "".
	if options.ResourceVersion != "" {
		rvs := strings.Split(options.ResourceVersion, "/")
		if len(rvs) != len(mlw) {
			return nil, fmt.Errorf("expected resource version to have %d parts to match the number of ListerWatchers", len(mlw))
		}
		resourceVersions = rvs
	}
	return newMultiWatch(mlw, resourceVersions, options)
}

// multiWatch abstracts multiple watch.Interface's, allowing them
// to be treated as a single watch.Interface.
type multiWatch struct {
	result   chan watch.Event
	stopped  chan struct{}
	stoppers []func()
}

// newMultiWatch returns a new multiWatch or an error if one of the underlying
// Watch funcs errored. The length of []cache.ListerWatcher and []string must
// match.
func newMultiWatch(lws []cache.ListerWatcher, resourceVersions []string, options metav1.ListOptions) (*multiWatch, error) {
	var (
		result   = make(chan watch.Event)
		stopped  = make(chan struct{})
		stoppers []func()
		wg       sync.WaitGroup
	)

	wg.Add(len(lws))

	for i, lw := range lws {
		o := options.DeepCopy()
		o.ResourceVersion = resourceVersions[i]
		w, err := lw.Watch(*o)
		if err != nil {
			return nil, err
		}

		go func() {
			defer wg.Done()

			for {
				event, ok := <-w.ResultChan()
				if !ok {
					return
				}

				select {
				case result <- event:
				case <-stopped:
					return
				}
			}
		}()
		stoppers = append(stoppers, w.Stop)
	}

	// result chan must be closed,
	// once all event sender goroutines exited.
	go func() {
		wg.Wait()
		close(result)
	}()

	return &multiWatch{
		result:   result,
		stoppers: stoppers,
		stopped:  stopped,
	}, nil
}

// ResultChan implements the watch.Interface interface.
func (mw *multiWatch) ResultChan() <-chan watch.Event {
	return mw.result
}

// Stop implements the watch.Interface interface.
// It stops all of the underlying watch.Interfaces and closes the backing chan.
// Can safely be called more than once.
func (mw *multiWatch) Stop() {
	select {
	case <-mw.stopped:
		// nothing to do, we are already stopped
	default:
		for _, stop := range mw.stoppers {
			stop()
		}
		close(mw.stopped)
	}
	return
}

// IsAllNamespaces checks if the given slice of namespaces
// contains only v1.NamespaceAll.
func IsAllNamespaces(namespaces []string) bool {
	return len(namespaces) == 1 && namespaces[0] == v1.NamespaceAll
}
