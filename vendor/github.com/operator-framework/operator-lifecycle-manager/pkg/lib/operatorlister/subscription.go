package operatorlister

import (
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	listers "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/listers/operators/v1alpha1"
)

type UnionSubscriptionLister struct {
	subscriptionListers map[string]listers.SubscriptionLister
	subscriptionLock    sync.RWMutex
}

// List lists all Subscriptions in the indexer.
func (usl *UnionSubscriptionLister) List(selector labels.Selector) (ret []*v1alpha1.Subscription, err error) {
	usl.subscriptionLock.RLock()
	defer usl.subscriptionLock.RUnlock()

	set := make(map[types.UID]*v1alpha1.Subscription)
	for _, cl := range usl.subscriptionListers {
		subscriptions, err := cl.List(selector)
		if err != nil {
			return nil, err
		}

		for _, subscription := range subscriptions {
			set[subscription.GetUID()] = subscription
		}
	}

	for _, subscription := range set {
		ret = append(ret, subscription)
	}

	return
}

// Subscriptions returns an object that can list and get Subscriptions.
func (usl *UnionSubscriptionLister) Subscriptions(namespace string) listers.SubscriptionNamespaceLister {
	usl.subscriptionLock.RLock()
	defer usl.subscriptionLock.RUnlock()

	// Check for specific namespace listers
	if cl, ok := usl.subscriptionListers[namespace]; ok {
		return cl.Subscriptions(namespace)
	}

	// Check for any namespace-all listers
	if cl, ok := usl.subscriptionListers[metav1.NamespaceAll]; ok {
		return cl.Subscriptions(namespace)
	}

	return &NullSubscriptionNamespaceLister{}
}

func (usl *UnionSubscriptionLister) RegisterSubscriptionLister(namespace string, lister listers.SubscriptionLister) {
	usl.subscriptionLock.Lock()
	defer usl.subscriptionLock.Unlock()

	if usl.subscriptionListers == nil {
		usl.subscriptionListers = make(map[string]listers.SubscriptionLister)
	}

	usl.subscriptionListers[namespace] = lister
}

func (l *operatorsV1alpha1Lister) RegisterSubscriptionLister(namespace string, lister listers.SubscriptionLister) {
	l.subscriptionLister.RegisterSubscriptionLister(namespace, lister)
}

func (l *operatorsV1alpha1Lister) SubscriptionLister() listers.SubscriptionLister {
	return l.subscriptionLister
}

// NullSubscriptionNamespaceLister is an implementation of a null SubscriptionNamespaceLister. It is
// used to prevent nil pointers when no SubscriptionNamespaceLister has been registered for a given
// namespace.
type NullSubscriptionNamespaceLister struct {
	listers.SubscriptionNamespaceLister
}

// List returns nil and an error explaining that this is a NullSubscriptionNamespaceLister.
func (n *NullSubscriptionNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.Subscription, err error) {
	return nil, fmt.Errorf("cannot list Subscriptions with a NullSubscriptionNamespaceLister")
}

// Get returns nil and an error explaining that this is a NullSubscriptionNamespaceLister.
func (n *NullSubscriptionNamespaceLister) Get(name string) (*v1alpha1.Subscription, error) {
	return nil, fmt.Errorf("cannot get Subscription with a NullSubscriptionNamespaceLister")
}
