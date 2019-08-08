//go:generate go run internal/cmd/gentypes/main.go

// Package jwa defines the various algorithm described in https://tools.ietf.org/html/rfc7518
package jwa

// Size returns the size of the EllipticCurveAlgorithm
func (crv EllipticCurveAlgorithm) Size() int {
	switch crv {
	case P256:
		return 32
	case P384:
		return 48
	case P521:
		return 66
	}
	return 0
}
