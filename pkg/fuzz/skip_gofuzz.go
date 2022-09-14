//go:build gofuzz

package fuzz

func (h Helper) skip(reason string) {
	panic(panicPrefix + reason)
}
