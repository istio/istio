package queueinformer

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
)

type ResourceQueueSet map[string]workqueue.RateLimitingInterface

func (r ResourceQueueSet) Requeue(name, namespace string) error {
	// We can build the key directly, will need to change if queue uses different key scheme
	key := fmt.Sprintf("%s/%s", namespace, name)

	if queue, ok := r[metav1.NamespaceAll]; len(r) == 1 && ok {
		queue.AddRateLimited(key)
		return nil
	}

	if queue, ok := r[namespace]; ok {
		queue.AddRateLimited(key)
		return nil
	}

	return fmt.Errorf("couldn't find queue for resource")
}

func (r ResourceQueueSet) Remove(name, namespace string) error {
	// We can build the key directly, will need to change if queue uses different key scheme
	key := fmt.Sprintf("%s/%s", namespace, name)

	if queue, ok := r[metav1.NamespaceAll]; len(r) == 1 && ok {
		queue.Forget(key)
		return nil
	}

	if queue, ok := r[namespace]; ok {
		queue.Forget(key)
		return nil
	}

	return fmt.Errorf("couldn't find queue for resource")
}
