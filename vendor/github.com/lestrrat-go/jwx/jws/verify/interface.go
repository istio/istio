package verify

import (
	"crypto/ecdsa"
	"crypto/rsa"

	"github.com/lestrrat-go/jwx/jws/sign"
)

type Verifier interface {
	// Verify checks whether the payload and signature are valid for
	// the given key.
	// `key` is the key used for verifying the payload, and is usually
	// the public key associated with the signature method. For example,
	// for `jwa.RSXXX` and `jwa.PSXXX` types, you need to pass the
	// `*"crypto/rsa".PublicKey` type.
	// Check the documentation for each verifier for details
	Verify(payload []byte, signature []byte, key interface{}) error
}

type rsaVerifyFunc func([]byte, []byte, *rsa.PublicKey) error

type RSAVerifier struct {
	verify rsaVerifyFunc
}

type ecdsaVerifyFunc func([]byte, []byte, *ecdsa.PublicKey) error

type ECDSAVerifier struct {
	verify ecdsaVerifyFunc
}

type HMACVerifier struct {
	signer sign.Signer
}
