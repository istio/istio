package event

import (
	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/scheme"
)

const component string = "operator-lifecycle-manager"

// NewRecorder returns an EventRecorder type that can be
// used to post Events to different object's lifecycles.
func NewRecorder(event typedcorev1.EventInterface) (record.EventRecorder, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: event})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: component})

	return recorder, nil
}
