package hash

import (
	"encoding/binary"
	"encoding/hex"
	"github.com/cespare/xxhash/v2"
	"hash"
)

type Instance struct {
	hash.Hash
}

func New() *Instance {
	return &Instance{
		xxhash.New(),
	}
}

// Write wraps the Hash.Write function call
// Hash.Write error always return nil, this func simplify caller handle error
func (i *Instance) Write(p []byte) (n int) {
	n, _ = i.Hash.Write(p)
	return
}

func (i *Instance) SumToUint64(b []byte) uint64 {
	sum := i.Sum(b)
	return binary.BigEndian.Uint64(sum)
}

func (i *Instance) SumToString(b []byte) string {
	sum := i.Sum(b)
	return hex.EncodeToString(sum)
}
