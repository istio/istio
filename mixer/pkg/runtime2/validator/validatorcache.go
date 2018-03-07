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

package validator

import (
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pkg/cache"
)

const validatedDataExpiration = time.Second * 10
const validatedDataEviction = time.Second * 3

// validatorCache is a cache of resources in store, but has some expiration
// logic on updating the store.
type validatorCache struct {
	c cache.ExpiringCache

	mu         sync.Mutex
	configData map[store.Key]proto.Message
}

func (c *validatorCache) applyChanges(evs []*store.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ev := range evs {
		if ev.Type == store.Delete {
			delete(c.configData, ev.Key)
		} else {
			c.configData[ev.Key] = ev.Value.Spec
		}
	}
}

func (c *validatorCache) get(key store.Key) (proto.Message, bool) {
	if value, ok := c.c.Get(key); ok {
		ev := value.(*store.Event)
		if ev.Type == store.Delete {
			return nil, false
		}
		return ev.Value.Spec, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	spec, ok := c.configData[key]
	if spec == nil {
		delete(c.configData, key)
		return nil, false
	}
	return spec, ok
}

func (c *validatorCache) forEach(proc func(key store.Key, spec proto.Message)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	toDelete := []store.Key{}
	for key, spec := range c.configData {
		if value, ok := c.c.Get(key); ok {
			ev := value.(*store.Event)
			if ev.Type == store.Delete {
				continue
			}
			proc(key, ev.Value.Spec)
		} else if spec == nil {
			toDelete = append(toDelete, key)
		} else {
			proc(key, spec)
		}
	}
	for _, key := range toDelete {
		delete(c.configData, key)
	}
}

func (c *validatorCache) putCache(ev *store.Event) {
	c.c.Set(ev.Key, ev)
	if ev.Type == store.Delete {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.configData[ev.Key]; !ok {
		c.configData[ev.Key] = nil
	}
}
