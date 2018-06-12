package zookeeper

import (
	"github.com/samuel/go-zookeeper/zk"
	"path"
)

type pathCacheEventType int

type pathCache struct {
	conn       *zk.Conn
	watchCh    chan zk.Event
	notifyCh   chan pathCacheEvent
	stopCh     chan bool
	addChildCh chan string
	path       string
	children   map[string]bool
}

type pathCacheEvent struct {
	eventType pathCacheEventType
	path      string
}

const (
	pathCacheEventAdded   pathCacheEventType = iota
	pathCacheEventDeleted
)

func newPathCache(conn *zk.Conn, path string) (*pathCache, error) {
	p := &pathCache{
		conn:     conn,
		path:     path,
		children: make(map[string]bool),

		watchCh:    make(chan zk.Event),
		notifyCh:   make(chan pathCacheEvent),
		addChildCh: make(chan string),
		stopCh:     make(chan bool),
	}

	err := p.watchChildren()
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			select {
			case child := <-p.addChildCh:
				p.onChildAdd(child)
			case event := <-p.watchCh:
				p.onEvent(&event)
			case <-p.stopCh:
				close(p.notifyCh)
				return
			}
		}
	}()

	return p, nil
}

func (p *pathCache) events() <-chan pathCacheEvent {
	return p.notifyCh
}

func (p *pathCache) stop() {
	go func() {
		p.stopCh <- true
	}()
}

func (p *pathCache) watch(path string) error {
	_, _, ch, err := p.conn.GetW(path)
	if err != nil {
		return err
	}
	go p.forward(ch)
	return nil
}

func (p *pathCache) watchChildren() error {
	children, _, ch, err := p.conn.ChildrenW(p.path)
	if err != nil {
		return err
	}
	go p.forward(ch)
	for _, child := range children {
		fp := path.Join(p.path, child)
		if ok := p.children[fp]; !ok {
			go p.addChild(fp)
		}
	}
	return nil
}

func (p *pathCache) onChildAdd(child string) {
	err := p.watch(child)
	if err != nil {
		return
	}
	p.children[child] = true
	event := pathCacheEvent{
		eventType: pathCacheEventAdded,
		path:      child,
	}
	go p.notify(event)
}

func (p *pathCache) onEvent(event *zk.Event) {
	switch event.Type {
	case zk.EventNodeChildrenChanged:
		p.watchChildren()
	case zk.EventNodeDeleted:
		p.onChildDeleted(event.Path)
	}
}

func (p *pathCache) onChildDeleted(child string) {
	vent := pathCacheEvent{
		eventType: pathCacheEventDeleted,
		path:      child,
	}
	go p.notify(vent)
}

func (p *pathCache) addChild(child string) {
	p.addChildCh <- child
}

func (p *pathCache) notify(event pathCacheEvent) {
	p.notifyCh <- event
}

func (p *pathCache) forward(eventCh <-chan zk.Event) {
	event, ok := <-eventCh
	if ok {
		p.watchCh <- event
	}
}
