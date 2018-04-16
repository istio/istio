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

package contextgraph

import (
	"fmt"
	"strings"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
)

type workload struct {
	namespace string
	wlType    string
	name      string
	id        string
	seen      time.Time
	group     string
}

type workloadCache struct {
	cache  map[string]workload
	logger adapter.Logger
}

func newWorkloadCache(l adapter.Logger) workloadCache {
	w := workloadCache{
		cache:  make(map[string]workload),
		logger: l,
	}
	return w
}

func (c workloadCache) InsertOrTrim(workloads []workload) []workload {
	toSend := make([]workload, 0)

	for _, wl := range workloads {
		w, ok := c.cache[wl.id]
		if ok {
			if w.seen.Before(wl.seen.Add(-(time.Minute * 9))) {
				// Sent before, but need to send again, reset lastSend
				c.cache[wl.id] = wl
				toSend = append(toSend, wl)
				c.logger.Debugf("%v was sent before, but need to send again, old time: %v, now seen time: %v", wl.id, w.seen, wl.seen)
			}
			//c.logger.Debugf("%v was sent before, still fresh, don't again, old time: %v, now seen time: %v", wl.id, w.seen, wl.seen)
			// Sent within the last 9 min
		}
		if !ok {
			// Never seen before
			c.cache[wl.id] = wl
			toSend = append(toSend, wl)
			c.logger.Debugf("%v never seen, send, now seen time: %v", wl.id, wl.seen)
		}
	}

	return toSend
}

func (c workloadCache) Check(wl workload) bool {
	w, ok := c.cache[wl.id]
	if ok {
		if w.seen.Before(wl.seen.Add(-(time.Minute * 9))) {
			// Sent before, but need to send again, reset lastSend
			c.cache[wl.id] = wl
			c.logger.Debugf("%v was sent before, but need to send again, old time: %v, now seen time: %v", wl.id, w.seen, wl.seen)
			return true
		}
		return false
		//c.logger.Debugf("%v was sent before, still fresh, don't again, old time: %v, now seen time: %v", wl.id, w.seen, wl.seen)
		// Sent within the last 9 min
	} else {
		// Never seen before
		c.cache[wl.id] = wl
		c.logger.Debugf("%v never seen, send, now seen time: %v", wl.id, wl.seen)
		return true
	}
}

func newWorkload(owner, wlName, wlNS string, ts time.Time) workload {
	wl := workload{
		namespace: wlNS,
		name:      wlName,
		seen:      ts,
	}
	t := strings.Split(strings.TrimSuffix(owner, fmt.Sprintf("/%s", wlName)), "/")
	wl.wlType = t[len(t)-1]
	wl.id = fmt.Sprintf("%s#%s#%s", wl.namespace, wl.wlType, wl.name)
	if strings.Contains(owner, "extensions") {
		wl.group = "extensions"
	} else if strings.Contains(owner, "apps") {
		wl.group = "apps"
	}
	return wl
}

type traffic struct {
	sourceId string
	destId   string
	id       string
	protocol string
	seen     time.Time
}

type trafficCache struct {
	cache map[string]traffic
}

func newTrafficCache() trafficCache {
	w := trafficCache{
		cache: make(map[string]traffic),
	}
	return w
}

func newTraffic(source, dest workload, cproto, aproto string, ts time.Time) traffic {
	t := traffic{
		sourceId: source.id,
		destId:   dest.id,
		seen:     ts,
	}
	if (cproto == "tcp") || (cproto == "http") {
		t.protocol = cproto
	} else {
		t.protocol = aproto
	}
	t.id = fmt.Sprintf("%s#%s#%s", source.id, dest.id, t.protocol)
	return t
}

func (c trafficCache) InsertOrTrim(traffics []traffic) []traffic {
	toSend := make([]traffic, 0)

	for _, tr := range traffics {
		t, ok := c.cache[tr.id]
		if ok {
			if t.seen.Before(tr.seen.Add(-(time.Minute * 9))) {
				// Sent before, but need to send again, reset lastSend
				c.cache[tr.id] = tr
				toSend = append(toSend, tr)
			}
			// Sent within the last 9 min
		}
		if !ok {
			// Never seen before
			c.cache[tr.id] = tr
			toSend = append(toSend, tr)
		}
	}

	return toSend
}

func (c trafficCache) Check(tr traffic) bool {
	t, ok := c.cache[tr.id]
	if ok {
		if t.seen.Before(tr.seen.Add(-(time.Minute * 9))) {
			// Sent before, but need to send again, reset lastSend
			c.cache[tr.id] = tr
			return true
		}
		// Sent within the last 9 min
		return false
	}
	if !ok {
		// Never seen before
		c.cache[tr.id] = tr
		return true
	}
	return false
}

func (c trafficCache) Invalid(w workload) {
	for id, t := range c.cache {
		if t.sourceId == w.id {
			delete(c.cache, id)
		}
	}
}
