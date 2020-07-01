package envoyfilter

import (
	"github.com/golang/protobuf/proto"
)

// TODO replace this with something thread safe that will evict unused entries
var clones = map[string]proto.Message{}

func memoClone(val proto.Message) proto.Message {
	h, err := proto.Marshal(val)
	if err != nil {
		return proto.Clone(val)
	}
	key := string(h)
	if _, ok := clones[key]; ok {
		return clones[key]
	}
	clones[key] = proto.Clone(val)
	return clones[key]
}
