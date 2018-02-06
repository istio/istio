package utils

import "sync"

type LoopFactor struct {
	track bool
	m     *sync.Mutex
}

func NewLoopFactor(x bool) *LoopFactor {
	return &LoopFactor{
		track: x,
		m:     new(sync.Mutex),
	}
}

func (l *LoopFactor) GetBool() bool {
	l.m.Lock()
	defer l.m.Unlock()
	return l.track
}

func (l *LoopFactor) SetBool(x bool) {
	l.m.Lock()
	defer l.m.Unlock()
	l.track = x
}
