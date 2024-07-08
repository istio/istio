package ambient

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/queue"
)

func ReportWaypointIsNotReady(waypoint string) *model.StatusMessage {
	return &model.StatusMessage{
		Reason:  "WaypointIsNotReady",
		Message: fmt.Sprintf("waypoint %q is not ready", waypoint),
	}
}

func ReportWaypointAttachmentDenied(waypoint string) *model.StatusMessage {
	return &model.StatusMessage{
		Reason:  "AttachmentDenied",
		Message: fmt.Sprintf("we are not permitted to attach to waypoint %q (missing allowedRoutes?)", waypoint),
	}
}

func ReportWaypointUnsupportedTrafficType(waypoint string, ttype string) *model.StatusMessage {
	return &model.StatusMessage{
		Reason:  "UnsupportedTrafficType",
		Message: fmt.Sprintf("attempting to bind to traffic type %q which the waypoint %q does not support", ttype, waypoint),
	}
}

type StatusQueue struct {
	queue queue.Instance
}

func NewStatusQueue() *StatusQueue {
	sq := &StatusQueue{}
	sq.queue = queue.NewQueueWithID(time.Second, "ambient status")
	// sq.queue = controllers.NewQueue("ambient status",
	//	controllers.WithGenericReconciler(sq.Reconcile),
	//	controllers.WithMaxAttempts(5))
	return sq
}

func (q *StatusQueue) Push(task queue.Task) {
	q.queue.Push(task)
}

func (q *StatusQueue) Run(stop <-chan struct{}) {
	q.queue.Run(stop)
}

type StatusItem struct {
	Patcher kclient.Patcher
	Object  types.NamespacedName
	Message model.StatusMessage
}
