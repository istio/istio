package factory

import (
	"github.com/cespare/xxhash/v2"
	"hash"
)

func NewHash() hash.Hash {
	return xxhash.New()
}
