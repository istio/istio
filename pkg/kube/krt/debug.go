package krt

import (
	"encoding/json"

	"istio.io/istio/pkg/slices"
)

var DebugCollections = []DebugCollection{}

type CollectionDump struct {
	// Map of output key -> output
	Outputs map[string]any `json:"outputs"`
	// Map of input key -> info
	Inputs map[string]InputDump `json:"inputs"`
}
type InputDump struct {
	Outputs      []string `json:"outputs"`
	Dependencies []string `json:"dependencies"`
}
type DebugCollection struct {
	name string
	dump func() CollectionDump
}

func (p DebugCollection) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"name":  p.name,
		"state": p.dump(),
	})
}

func RegisterCollectionForDebugging[T any](c Collection[T]) {
	cc := c.(internalCollection[T])
	DebugCollections = append(DebugCollections, DebugCollection{
		name: cc.name(),
		dump: cc.dump,
	})
}

func eraseSlice[T any](l []T) []any {
	return slices.Map(l, func(e T) any {
		return any(e)
	})
}

func eraseMap[T any](l map[Key[T]]T) map[string]any {
	nm := make(map[string]any, len(l))
	for k, v := range l {
		nm[string(k)] = v
	}
	return nm
}
