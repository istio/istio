package watchers

import (
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	api "k8s.io/api/core/v1"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/apimachinery/pkg/labels"
)

type PodUpdate struct {
	Pod *api.Pod
	Op      Operation
}

type PodWatcher struct {
	podController cache.Controller
	broadcaster        *Broadcaster
	informer      cache.SharedIndexInformer
	podLister     cache.Indexer
}

type PodUpdatesHandler interface {
	OnPodUpdate(podUpdate *PodUpdate)
}

func (pw *PodWatcher) addEventHandler(obj interface{}) {
	pod, ok := obj.(*api.Pod)
	if !ok {
		return
	}
	pw.broadcaster.Notify(&PodUpdate{Op: ADD, Pod: pod})
}

func (pw *PodWatcher) deleteEventHandler(obj interface{}) {
	pod, ok := obj.(*api.Pod)
	if !ok {
		return
	}
	pw.broadcaster.Notify(&PodUpdate{Op: REMOVE, Pod: pod})
}

func (pw *PodWatcher) updateEventHandler(oldObj, npwObj interface{}) {
	pod, ok := npwObj.(*api.Pod)
	if !ok {
		return
	}
	if !reflect.DeepEqual(npwObj, oldObj) {
		pw.broadcaster.Notify(&PodUpdate{Op: UPDATE, Pod: pod})
	}
}

func (pw *PodWatcher) RegisterHandler(handler PodUpdatesHandler) {
	pw.broadcaster.Add(ListenerFunc(func(instance interface{}) {
		handler.OnPodUpdate(instance.(*PodUpdate))
	}))
}

func (pw *PodWatcher) ListBySelector(set map[string]string) (ret []*api.Pod, err error) {
	/*
	store := pw.informer.GetStore()
	objList := store.List()

	pods := make([]*api.Pod, 0)
	for _, ins := range objList {
		pod := ins.(*api.Pod)
		if pod.Namespace != namespace {
			continue
		}

		//glog.V(3).Infof("pod %s/%s label %v", pod.Namespace, pod.Name, pod.Labels)

		if IsMapContain(selector, pod.Labels) {
			pods = append(pods, pod)
		}
	}
	return pods
	*/
	selector := labels.SelectorFromSet(set)
	err = cache.ListAll(pw.podLister, selector, func(m interface{}) {
		ret = append(ret, m.(*api.Pod))
	})
	return ret, err
}

func (pw *PodWatcher) HasSynced() bool {
	return pw.podController.HasSynced()
}

// StartPodWatcher: start watching updates for pods from Kuberentes API server
func StartPodWatcher(clientset kubernetes.Interface, resyncPeriod time.Duration, stopCh <- chan struct{}) (*PodWatcher, error) {
	pw := PodWatcher{}

	eventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    pw.addEventHandler,
		DeleteFunc: pw.deleteEventHandler,
		UpdateFunc: pw.updateEventHandler,
	}

	pw.broadcaster = NewBroadcaster()
	lw := cache.NewListWatchFromClient(clientset.Core().RESTClient(), "pods", metav1.NamespaceAll, fields.Everything())

	/*
	pw.informer = cache.NewSharedIndexInformer(
		lw,
		&api.Pod{}, resyncPeriod, cache.Indexers{},
	)
	pw.informer.AddEventHandler(eventHandler)
	go pw.informer.Run(stopCh)
	*/

	pw.podLister, pw.podController = cache.NewIndexerInformer(
		lw,
		&api.Pod{}, resyncPeriod, eventHandler,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	go pw.podController.Run(stopCh)

	return &pw, nil
}
