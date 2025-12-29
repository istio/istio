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

package kclient

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"istio.io/istio/pkg/kube"
)

type EventRecorder struct {
	eventRecorder    record.EventRecorder
	eventBroadcaster record.EventBroadcaster
}

// NewEventRecorder creates a new EventRecorder.
// This should be shutdown after usage.
func NewEventRecorder(client kube.Client, component string) EventRecorder {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.V(5).Infof) // Will log at kube:debug level
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: client.Kube().CoreV1().Events("")})
	eventRecorder := eventBroadcaster.NewRecorder(kube.IstioScheme, corev1.EventSource{
		Component: component,
	})
	return EventRecorder{
		eventRecorder:    eventRecorder,
		eventBroadcaster: eventBroadcaster,
	}
}

// Write creates a single event.
func (e *EventRecorder) Write(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	e.eventRecorder.Eventf(object, eventtype, reason, messageFmt, args...)
}

// Shutdown terminates the event recorder. This must be called upon completion of writing events, and events should not be
// written once terminated.
func (e *EventRecorder) Shutdown() {
	e.eventBroadcaster.Shutdown()
}
