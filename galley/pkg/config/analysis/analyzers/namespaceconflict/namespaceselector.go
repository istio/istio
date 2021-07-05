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

package namespaceconflict

import (
	"fmt"

	k8s_labels "k8s.io/apimachinery/pkg/labels"

	v1beta1 "istio.io/api/security/v1beta1"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// Analyzer checks conditions related to conflicting namespace level resources
// Validates that there's only a single rule associated with a resource.
type Analyzer struct{}

var _ analysis.Analyzer = &Analyzer{}

var (
	peerAuthCollection    = collections.IstioSecurityV1Beta1Peerauthentications.Name()
	requestAuthCollection = collections.IstioSecurityV1Beta1Requestauthentications.Name()
)

func (a *Analyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "namespaceconflict.Analyzer",
		Description: "Checks conditions related to conflicting namespace level resources",
		Inputs: collection.Names{
			peerAuthCollection,
			requestAuthCollection,
		},
	}
}

func (a *Analyzer) Analyze(c analysis.Context) {
	// Analyze collections.IstioSecurityV1Beta1Peerauthentications
	c.ForEach(peerAuthCollection, func(r *resource.Instance) bool {
		x := r.Message.(*v1beta1.PeerAuthentication)
		if x.GetSelector() != nil {
			// If the resource has workloads associated with it, analyze for conflicts with selector
			a.analyzeWorkloadSelectorConflicts(r, c)
		} else {
			// If the resource has workloads associated with it, analyze for conflicts with namespace
			a.analyzeNamespaceWideConflicts(r, c)
		}
		return true
	})

	// Analyze collections.IstioSecurityV1Beta1Requestauthentications
	c.ForEach(requestAuthCollection, func(r *resource.Instance) bool {
		x := r.Message.(*v1beta1.RequestAuthentication)
		if x.GetSelector() != nil {
			// If the resource has workloads associated with it, analyze for conflicts with selector
			a.analyzeWorkloadSelectorConflicts(r, c)
		} else {
			// If the resource has workloads associated with it, analyze for conflicts with namespace
			a.analyzeNamespaceWideConflicts(r, c)
		}
		return true
	})
}

func (a *Analyzer) analyzeNamespaceWideConflicts(r *resource.Instance, c analysis.Context) {
	matches := a.findMatchingNamespace(r, c)
	// There should be only one resource associated with a selector
	if len(matches) != 0 {

		// The namespace in which we will throw the conflict
		xNS := r.Metadata.FullName.Namespace

		// Cast the message to it's respective type to report the issue correctly
		switch v := r.Message.(type) {
		case *v1beta1.PeerAuthentication:
			// Throw a Peer Authentication conflict
			// x := r.Message.(*v1beta1.PeerAuthentication)
			m := msg.NewNamespaceResourceConflict(r, peerAuthCollection.String(),
				string(xNS), fmt.Sprintf("(ALL) Namespace: %v", xNS),
				matches.SortedList())
			c.Report(collections.IstioSecurityV1Beta1Peerauthentications.Name(), m)
			return
		case *v1beta1.RequestAuthentication:
			// Throw an Authorization Policy conflict
			// x := r.Message.(*v1beta1.RequestAuthentication)
			m := msg.NewNamespaceResourceConflict(r, requestAuthCollection.String(),
				string(xNS), fmt.Sprintf("(ALL) Namespace: %v", xNS),
				matches.SortedList())
			c.Report(collections.IstioSecurityV1Beta1Requestauthentications.Name(), m)
			return
		default:
			fmt.Printf("I don't know about type %T!\n", v)
		}
	}
}

func (a *Analyzer) findMatchingNamespace(r *resource.Instance, c analysis.Context) sets.Set {
	matches := sets.NewSet()

	switch v := r.Message.(type) {

	case *v1beta1.PeerAuthentication:
		xName := r.Metadata.FullName.String()
		xNS := r.Metadata.FullName.Namespace
		c.ForEach(peerAuthCollection, func(r1 *resource.Instance) bool {
			y := r1.Message.(*v1beta1.PeerAuthentication)
			yName := r1.Metadata.FullName.String()
			yNS := r1.Metadata.FullName.Namespace
			if y.GetSelector() == nil && xName != yName && xNS == yNS {
				matches.Insert(xName)
				matches.Insert(yName)
			}
			return true
		})

	case *v1beta1.RequestAuthentication:
		xName := r.Metadata.FullName.String()
		xNS := r.Metadata.FullName.Namespace
		c.ForEach(requestAuthCollection, func(r1 *resource.Instance) bool {
			y := r1.Message.(*v1beta1.RequestAuthentication)
			yName := r1.Metadata.FullName.String()
			yNS := r1.Metadata.FullName.Namespace
			if y.GetSelector() == nil && xName != yName && xNS == yNS {
				matches.Insert(xName)
				matches.Insert(yName)
			}
			return true
		})

	default:
		fmt.Printf("I don't know about type %T!\n", v)
	}

	return matches
}

func (a *Analyzer) analyzeWorkloadSelectorConflicts(r *resource.Instance, c analysis.Context) {
	// Find all resources that have the same selector
	matches := a.findMatchingSelectors(r, c)

	// There should be only one resource associated with a selector
	if len(matches) != 0 {

		// The namespace in which we will throw the conflict
		xNS := r.Metadata.FullName.Namespace

		// Cast the message to it's respective type to report the issue correctly
		switch v := r.Message.(type) {
		case *v1beta1.PeerAuthentication:
			// Throw a Peer Authentication conflict
			x := r.Message.(*v1beta1.PeerAuthentication)
			m := msg.NewNamespaceResourceConflict(r, peerAuthCollection.String(),
				string(xNS), k8s_labels.SelectorFromSet(x.GetSelector().MatchLabels).String(),
				matches.SortedList())
			c.Report(collections.IstioSecurityV1Beta1Peerauthentications.Name(), m)
			return
		case *v1beta1.RequestAuthentication:
			// Throw an Authorization Policy conflict
			x := r.Message.(*v1beta1.RequestAuthentication)
			m := msg.NewNamespaceResourceConflict(r, requestAuthCollection.String(),
				string(xNS), k8s_labels.SelectorFromSet(x.GetSelector().MatchLabels).String(),
				matches.SortedList())
			c.Report(collections.IstioSecurityV1Beta1Requestauthentications.Name(), m)
			return
		default:
			fmt.Printf("Incorrect collection iterated: %T.\n", v)
		}
	}
}

// Finds all resources that have the same selector as the resource we're checking
func (a *Analyzer) findMatchingSelectors(r *resource.Instance, c analysis.Context) sets.Set {
	matches := sets.NewSet()

	switch v := r.Message.(type) {

	case *v1beta1.PeerAuthentication:
		x := r.Message.(*v1beta1.PeerAuthentication)
		xName := r.Metadata.FullName.String()
		xNS := r.Metadata.FullName.Namespace
		xSelector := k8s_labels.SelectorFromSet(x.GetSelector().MatchLabels).String()
		c.ForEach(peerAuthCollection, func(r1 *resource.Instance) bool {
			y := r1.Message.(*v1beta1.PeerAuthentication)
			yName := r1.Metadata.FullName.String()
			yNS := r1.Metadata.FullName.Namespace
			if y.GetSelector() != nil {
				ySelector := k8s_labels.SelectorFromSet(y.GetSelector().MatchLabels).String()
				if xSelector == ySelector && xName != yName && xNS == yNS {
					matches.Insert(xName)
					matches.Insert(yName)
				}
			}
			return true
		})

	case *v1beta1.RequestAuthentication:
		x := r.Message.(*v1beta1.RequestAuthentication)
		xName := r.Metadata.FullName.String()
		xNS := r.Metadata.FullName.Namespace
		xSelector := k8s_labels.SelectorFromSet(x.GetSelector().MatchLabels).String()
		c.ForEach(requestAuthCollection, func(r1 *resource.Instance) bool {
			y := r1.Message.(*v1beta1.RequestAuthentication)
			yName := r1.Metadata.FullName.String()
			yNS := r1.Metadata.FullName.Namespace

			if y.GetSelector() != nil {
				ySelector := k8s_labels.SelectorFromSet(y.GetSelector().MatchLabels).String()
				if xSelector == ySelector && xName != yName && xNS == yNS {
					matches.Insert(xName)
					matches.Insert(yName)
				}
			}
			return true
		})

	default:
		fmt.Printf("Incorrect collection iterated: %T.\n", v)
	}

	return matches
}
