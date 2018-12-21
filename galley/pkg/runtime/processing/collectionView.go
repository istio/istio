//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import (
	"fmt"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// ToEnvelopeFn converts an interface{} to an MCP envelope.
type ToEnvelopeFn func(iface interface{}) (*mcp.Envelope, error)

// CollectionView is a View implementation, backed by a collection.
type CollectionView struct {
	typeURL    resource.TypeURL
	xform      ToEnvelopeFn
	collection *Collection
}

var _ View = &CollectionView{}

// NewCollectionView returns a CollectionView backed by the given Collection instance.
func NewCollectionView(typeURL resource.TypeURL, c *Collection, xform ToEnvelopeFn) *CollectionView {
	if xform == nil {
		xform = func(iface interface{}) (*mcp.Envelope, error) {
			e, ok := iface.(*mcp.Envelope)
			if !ok {
				return nil, fmt.Errorf("unable to convert object to envelope: %v", iface)
			}
			return e, nil
		}
	}
	return &CollectionView{
		xform: xform,
		typeURL:    typeURL,
		collection: c,
	}
}

// Type implements View
func (v *CollectionView) Type() resource.TypeURL {
	return v.typeURL
}

// Generation implements View
func (v *CollectionView) Generation() int64 {
	return v.collection.Generation()
}

// Get implements View
func (v *CollectionView) Get() []*mcp.Envelope {
	result := make([]*mcp.Envelope, 0, v.collection.Count())

	v.collection.ForEachItem(func(iface interface{}) {
		e, err := v.xform(iface)
		if err != nil {
			scope.Errorf("CollectionView: %v", err)
			return
		}
		result = append(result, e)
	})

	return result
}
