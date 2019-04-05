package plaintext

import (
	"regexp"
)

// StripTemplate is a WIP on remove golang template markup from a file
func StripTemplate(raw []byte) []byte {
	r, err := regexp.Compile(`({{[^}]+}})`)
	if err != nil {
		panic(err)
	}
	return r.ReplaceAllLiteral(raw, []byte{0x20})
}
