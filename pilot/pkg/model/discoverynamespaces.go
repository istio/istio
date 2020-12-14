package model

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	v1 "k8s.io/client-go/listers/core/v1"
)

const (
	// PilotDiscoveryLabelName is the name of the label used to indicate a namespace for discovery
	PilotDiscoveryLabelName = "istio-discovery"
	// PilotDiscoveryLabelValue is the value of for the label PilotDiscoveryLabelName used to indicate a namespace for discovery
	PilotDiscoveryLabelValue = "true"
)

func GetDiscoveryNamespaces(lister v1.NamespaceLister) sets.String {
	selector := labels.Set(map[string]string{PilotDiscoveryLabelName: PilotDiscoveryLabelValue}).AsSelector()
	namespaceList, err := lister.List(selector)
	if err != nil {
		log.Errorf("failed to get namespaces: %v", err)
		return nil
	}
	discoveryNamespaces := sets.NewString()
	for _, ns := range namespaceList {
		discoveryNamespaces.Insert(ns.Name)
	}
	return discoveryNamespaces
}
