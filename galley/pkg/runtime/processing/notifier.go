//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

// notifier listens to Views and notifies Listeners
type notifier struct {
	listeners []Listener
}

var _ ViewListener = &notifier{}

func newNotifier(listeners []Listener, views []View) *notifier {
	n := &notifier {
		listeners: listeners,
	}

	for _, v := range views {
		v.SetViewListener(n)
	}

	return n
}

func (n *notifier) ViewChanged(v View) {
	for _, listener := range n.listeners {
		listener.Changed(v.Type())
	}
}