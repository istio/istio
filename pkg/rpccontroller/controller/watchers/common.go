package watchers

import (
	"sync"
)

type Operation int

const (
	ADD Operation = iota
	UPDATE
	REMOVE
	SYNCED
)

var (
	OperationString = []string{"ADD", "UPDATE", "REMOVE", "SYNCED"}
)

type Listener interface {
	OnUpdate(instance interface{})
}

type ListenerFunc func(instance interface{})

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

func IsMapContain(map1, map2 map[string]string) bool {
	for k,v := range(map1) {
		v1, exist := map2[k]
		if !exist || v1 != v {
			return false
		}
	}

	return true
}

