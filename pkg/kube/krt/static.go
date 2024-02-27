package krt

import "istio.io/istio/pkg/maps"

type staticList[T any] struct {
	vals map[Key[T]]T
}

func NewStaticCollection[T any](vals []T) Collection[T] {
	res := map[Key[T]]T{}
	for _, v := range vals {
		res[GetKey(v)] = v
	}
	return &staticList[T]{res}
}

func (s *staticList[T]) GetKey(k Key[T]) *T {
	if o, f := s.vals[k]; f {
		return &o
	}
	return nil
}

func (s *staticList[T]) name() string {
	return "staticList"
}

func (s *staticList[T]) dump() {
}

func (s *staticList[T]) augment(a any) any {
	return a
}

func (s *staticList[T]) List(namespace string) []T {
	if namespace == "" {
		return maps.Values(s.vals)
	}
	var res []T
	// Future improvement: shard outputs by namespace so we can query more efficiently
	for _, v := range s.vals {
		if getNamespace(v) == namespace {
			res = append(res, v)
		}
	}
	return res
}

func (s *staticList[T]) Register(f func(o Event[T])) Syncer {
	panic("StaticCollection does not support event handlers")
}

func (s *staticList[T]) Synced() Syncer {
	return alwaysSynced{}
}

func (s *staticList[T]) RegisterBatch(f func(o []Event[T]), runExistingState bool) Syncer {
	panic("StaticCollection does not support event handlers")
}

var _ internalCollection[any] = &staticList[any]{}
