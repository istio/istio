//go:build !gofuzz

package fuzz

func (h Helper) skip(reason string) {
	h.t.Skip(reason)
}
