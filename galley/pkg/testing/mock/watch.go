//  Copyright 2018 Istio Authors
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

package mock

import "k8s.io/apimachinery/pkg/watch"

// Watch is a mock implementation of watch.Interface.
type Watch struct {
	ch chan watch.Event
}

var _ watch.Interface = &Watch{}

// NewWatch returns a new Watch instance.
func NewWatch() *Watch {
	return &Watch{
		make(chan watch.Event),
	}
}

// Stop is an implementation of watch.Interface.Watch.
func (w *Watch) Stop() {
	close(w.ch)
}

// ResultChan is an implementation of watch.Interface.ResultChan.
func (w *Watch) ResultChan() <-chan watch.Event {
	return w.ch
}

// Send a watch event through the result channel.
func (w *Watch) Send(event watch.Event) {
	w.ch <- event
}
