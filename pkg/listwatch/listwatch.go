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

// Provides multiple namespace listerWatcher. This implementation is Largely from
// https://github.com/coreos/prometheus-operator/pkg/listwatch/listwatch.go

package listwatch

import (
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// MultiNamespaceListerWatcher takes a list of namespaces and a
// cache.ListerWatcher generator func and returns a single cache.ListerWatcher
// capable of operating on multiple namespaces.
func MultiNamespaceListerWatcher(namespaces []string, f func(string) cache.ListerWatcher) cache.ListerWatcher {
	// If there is only one namespace then there is no need to create a proxy.
	if len(namespaces) == 1 {
		return f(namespaces[0])
	}
	lws := make([]cache.ListerWatcher, 0, len(namespaces))
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
	resourceVersions := make([]string, 0, len(mlw))
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
		stoppers = make([]func(), 0, len(lws))
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
}
