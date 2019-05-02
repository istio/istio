package catalog

import (
	"errors"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
)

var (
	ErrNilSubscription = errors.New("invalid Subscription object: <nil>")
)

const (
	PackageLabel          = "olm.package"
	CatalogLabel          = "olm.catalog"
	CatalogNamespaceLabel = "olm.catalog.namespace"
	ChannelLabel          = "olm.channel"
)

func labelsForSubscription(sub *v1alpha1.Subscription) map[string]string {
	return map[string]string{
		PackageLabel:          sub.Spec.Package,
		CatalogLabel:          sub.Spec.CatalogSource,
		CatalogNamespaceLabel: sub.Spec.CatalogSourceNamespace,
		ChannelLabel:          sub.Spec.Channel,
	}
}

// TODO remove this once UI no longer needs them
func legacyLabelsForSubscription(sub *v1alpha1.Subscription) map[string]string {
	return map[string]string{
		"alm-package": sub.Spec.Package,
		"alm-catalog": sub.Spec.CatalogSource,
		"alm-channel": sub.Spec.Channel,
	}
}

func ensureLabels(sub *v1alpha1.Subscription) *v1alpha1.Subscription {
	labels := sub.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range labelsForSubscription(sub) {
		labels[k] = v
	}
	for k, v := range legacyLabelsForSubscription(sub) {
		labels[k] = v
	}
	sub.SetLabels(labels)
	return sub
}
