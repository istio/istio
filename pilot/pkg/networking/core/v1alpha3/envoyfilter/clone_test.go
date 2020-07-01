package envoyfilter

import (
	"github.com/golang/protobuf/proto"
	"testing"
)

var p = buildPatchStruct(`{"name": "http-filter4"}`)

var clone proto.Message

func BenchmarkClone(b *testing.B) {
	var c proto.Message
	for i := 0; i < b.N; i++ {
		c = proto.Clone(p)
	}
	clone = c
}


var marshaled []byte

func BenchmarkMarshal(b *testing.B) {
	var m []byte
	for i := 0; i < b.N; i++ {
		m, _ = proto.Marshal(p)
	}
	marshaled = m
}
