// Copyright 2017 Istio Authors
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

// This file contains an implementation of an opentracing carrier built on top of the mixer's attribute bag. This allows
// clients to inject their span metadata into the set of attributes its reporting to the mixer, and lets the mixer extract
// span information propagated as attributes in a standard call to the mixer.

package tracing

import (
	"context"
	"strings"

	"istio.io/mixer/pkg/attribute"
)

const (
	// TODO: elevate this to a global registry, especially since non-go clients (the proxy) will need to use the same prefix
	prefix       = ":istio-ot-"
	prefixLength = len(prefix)
)

// AttributeCarrier implements opentracing's TextMapWriter and TextMapReader interfaces using Istio's attribute system.
type AttributeCarrier struct {
	mb attribute.MutableBag
}

// NewCarrier initializes a carrier that can be used to modify the attributes in the current request context.
func NewCarrier(bag attribute.MutableBag) *AttributeCarrier {
	return &AttributeCarrier{mb: bag.Child()}
}

type carrierKey struct{}

// NewContext annotates the provided context with the provided carrier and returns the updated context.
func NewContext(ctx context.Context, carrier *AttributeCarrier) context.Context {
	return context.WithValue(ctx, carrierKey{}, carrier)
}

// FromContext extracts the AttributeCarrier from the provided context, or returns nil if one is not found.
func FromContext(ctx context.Context) *AttributeCarrier {
	mc := ctx.Value(carrierKey{})
	if c, ok := mc.(*AttributeCarrier); ok {
		return c
	}
	return nil
}

// ForeachKey iterates over the keys that correspond to string attributes, filtering keys for the prefix used by the
// AttributeCarrier to denote opentracing attributes. The AttributeCarrier specific prefix is stripped from the key
// before its passed to handler.
func (c *AttributeCarrier) ForeachKey(handler func(key, val string) error) error {
	keys := c.mb.StringKeys()
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			if val, found := c.mb.String(key); found {
				if err := handler(key[prefixLength:], val); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Set adds (key, val) to the set of string attributes in the request scope. It prefixes the key with an istio-private
// string to avoid collisions with other attributes when arbitrary tracer implementations are used.
func (c *AttributeCarrier) Set(key, val string) {
	c.mb.SetString(prefix+key, val)
}
