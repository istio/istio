package controller

import (
	"go.uber.org/atomic"
	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"
	"k8s.io/client-go/tools/cache"
)

var (
	errors = monitoring.NewSum(
		"controller_sync_errors",
		"Total number of errors syncing controllers.",
	)

	recoveries = monitoring.NewSum(
		"controller_sync_recoveries",
		"Total number of recoveries from controller sync errors.",
	)

	controllerName = monitoring.MustCreateLabel("controller")
)

type Informer interface {
	AddEventHandler(handler cache.ResourceEventHandler)
	SetWatchErrorHandler(func(r *cache.Reflector, err error)) error
}

type InformerErrHandler interface {
	cache.ResourceEventHandler
	OnError(r *cache.Reflector, err error)
}

type informerErrHandler struct {
	controller string
	lastErr    *atomic.Error
	errors     monitoring.Metric
	recoveries monitoring.Metric
}

func (h *informerErrHandler) clearError(reason string) {
	if h.lastErr.Load() != nil {
		h.lastErr.Store(nil)
		h.recoveries.Increment()
		log.Infof("%s recovered (%s event)", h.controller, reason)
	}
}

// OnAdd emits a metric and logs reocvery if an error has occurred since the last recovery.
func (h *informerErrHandler) OnAdd(_ interface{}) {
	h.clearError("add")
}

// OnUpdate emits a metric and logs reocvery if an error has occurred since the last recovery.
func (h *informerErrHandler) OnUpdate(_, _ interface{}) {
	h.clearError("update")
}

// OnDelete emits a metric and logs reocvery if an error has occurred since the last recovery.
func (h *informerErrHandler) OnDelete(_ interface{}) {
	h.clearError("delete")
}

// OnError emits an error metric and logs an error.
func (h *informerErrHandler) OnError(_ *cache.Reflector, err error) {
	h.lastErr.Store(err)
	h.errors.Increment()
	log.Errorf("error in watch for %s: %v", h.controller, err)
}

// NewInformerErrorHandler creates an InformerErrHandler and registers metrics for errors
// and recovery labeled with controller name and any custom labels provided.
func NewInformerErrorHandler(name string, labels ...monitoring.LabelValue) InformerErrHandler {
	h := &informerErrHandler{
		controller: name,
		lastErr:    atomic.NewError(nil),
		errors:     errors.With(controllerName.Value(name)).With(labels...),
		recoveries: recoveries.With(controllerName.Value(name)).With(labels...),
	}
	for _, m := range []monitoring.Metric{h.errors, h.recoveries} {
		if err := m.Register(); err != nil {
			log.Warnf("failed registering metric %s for controller %s: %v", m.Name(), name, err)
		}
	}
	return h
}

// CollectInformerMetrics creates a new InformerErrorHandler and registers it with the given informers.
// This will replace the WatchErrorHandler on each informer. To use InformerErrorHandler alongside a custom
// handler, use NewInformerErrorhandler and call OnError from within the custom handler.
func CollectInformerMetrics(name string, informer Informer, labels ...monitoring.LabelValue) {
	h := NewInformerErrorHandler(name, labels...)
	informer.AddEventHandler(h)
	if err := informer.SetWatchErrorHandler(h.OnError); err != nil {
		log.Warnf("failed registering error handler for informer on controller %s: %v", name, err)
	}
}
