package queueinformer

import (
	"github.com/operator-framework/operator-lifecycle-manager/pkg/metrics"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// SyncHandler is the function that reconciles the controlled object when seen
type SyncHandler func(obj interface{}) error

// QueueInformer ties an informer to a queue in order to process events from the informer
// the informer watches objects of interest and adds objects to the queue for processing
// the syncHandler is called for all objects on the queue
type QueueInformer struct {
	queue                     workqueue.RateLimitingInterface
	informer                  cache.SharedIndexInformer
	syncHandler               SyncHandler
	resourceEventHandlerFuncs *cache.ResourceEventHandlerFuncs
	name                      string
	metrics.MetricsProvider
	log *logrus.Logger
}

// enqueue adds a key to the queue. If obj is a key already it gets added directly.
// Otherwise, the key is extracted via keyFunc.
func (q *QueueInformer) enqueue(obj interface{}) {
	if obj == nil {
		return
	}

	key, ok := obj.(string)
	if !ok {
		key, ok = q.keyFunc(obj)
		if !ok {
			return
		}
	}

	q.queue.Add(key)
}

// keyFunc turns an object into a key for the queue. In the future will use a (name, namespace) struct as key
func (q *QueueInformer) keyFunc(obj interface{}) (string, bool) {
	k, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		q.log.Infof("creating key failed: %s", err)
		return k, false
	}

	return k, true
}

func (q *QueueInformer) defaultAddFunc(obj interface{}) {
	key, ok := q.keyFunc(obj)
	if !ok {
		q.log.Warnf("couldn't add %v, couldn't create key", obj)
		return
	}

	q.enqueue(key)
}

func (q *QueueInformer) defaultDeleteFunc(obj interface{}) {
	key, ok := q.keyFunc(obj)
	if !ok {
		q.log.Warnf("couldn't delete %v, couldn't create key", obj)
		return
	}

	q.queue.Forget(key)
}

func (q *QueueInformer) defaultUpdateFunc(oldObj, newObj interface{}) {
	key, ok := q.keyFunc(newObj)
	if !ok {
		q.log.Warnf("couldn't update %v, couldn't create key", newObj)
		return
	}

	q.enqueue(key)
}

// defaultResourceEventhandlerFuncs provides the default implementation for responding to events
// these simply Log the event and add the object's key to the queue for later processing
func (q *QueueInformer) defaultResourceEventHandlerFuncs() *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc:    q.defaultAddFunc,
		DeleteFunc: q.defaultDeleteFunc,
		UpdateFunc: q.defaultUpdateFunc,
	}
}

// New creates a set of new queueinformers given a name, a set of informers, and a sync handler to handle the objects
// that the operator is managing. Optionally, custom event handler funcs can be passed in (defaults will be provided)
func New(queue workqueue.RateLimitingInterface, informers []cache.SharedIndexInformer, handler SyncHandler, funcs *cache.ResourceEventHandlerFuncs, name string, metrics metrics.MetricsProvider, logger *logrus.Logger) []*QueueInformer {
	queueInformers := []*QueueInformer{}
	for _, informer := range informers {
		queueInformers = append(queueInformers, NewInformer(queue, informer, handler, funcs, name, metrics, logger))
	}
	return queueInformers
}

// NewInformer creates a new queueinformer given a name, an informer, and a sync handler to handle the objects
// that the operator is managing. Optionally, custom event handler funcs can be passed in (defaults will be provided)
func NewInformer(queue workqueue.RateLimitingInterface, informer cache.SharedIndexInformer, handler SyncHandler, funcs *cache.ResourceEventHandlerFuncs, name string, metrics metrics.MetricsProvider, logger *logrus.Logger) *QueueInformer {
	queueInformer := &QueueInformer{
		queue:           queue,
		informer:        informer,
		syncHandler:     handler,
		name:            name,
		MetricsProvider: metrics,
		log:             logger,
	}
	queueInformer.resourceEventHandlerFuncs = queueInformer.defaultResourceEventHandlerFuncs()
	if funcs != nil {
		if funcs.AddFunc != nil {
			queueInformer.resourceEventHandlerFuncs.AddFunc = funcs.AddFunc
		}
		if funcs.DeleteFunc != nil {
			queueInformer.resourceEventHandlerFuncs.DeleteFunc = funcs.DeleteFunc
		}
		if funcs.UpdateFunc != nil {
			queueInformer.resourceEventHandlerFuncs.UpdateFunc = funcs.UpdateFunc
		}
	}
	queueInformer.informer.AddEventHandler(queueInformer.resourceEventHandlerFuncs)
	return queueInformer
}
