/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package watchers

import (
	"sync"
)

// Operation type for K8S resource
type Operation int

// Operation enum
const (
	ADD Operation = iota
	UPDATE
	REMOVE
	SYNCED
)

var (
	// OperationString array for Operation type
	OperationString = []string{"ADD", "UPDATE", "REMOVE", "SYNCED"}
)

// Listener interface for K8S resource
type Listener interface {
	OnUpdate(instance interface{})
}

// ListenerFunc is callback of listener
type ListenerFunc func(instance interface{})

// OnUpdate callback
func (f ListenerFunc) OnUpdate(instance interface{}) {
	f(instance)
}

// Broadcaster holds the details of registered listeners
type Broadcaster struct {
	listenerLock sync.RWMutex
	listeners    []Listener
}

// NewBroadcaster returns an instance of Broadcaster object
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{}
}

// Add lets to register a listener
func (b *Broadcaster) Add(listener Listener) {
	b.listenerLock.Lock()
	defer b.listenerLock.Unlock()
	b.listeners = append(b.listeners, listener)
}

// Notify notifies an update to registered listeners
func (b *Broadcaster) Notify(instance interface{}) {
	b.listenerLock.RLock()
	listeners := b.listeners
	b.listenerLock.RUnlock()
	for _, listener := range listeners {
		go listener.OnUpdate(instance)
	}
}
