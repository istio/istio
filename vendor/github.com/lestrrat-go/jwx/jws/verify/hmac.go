package verify

import (
	"crypto/hmac"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jws/sign"
	"github.com/pkg/errors"
)

func newHMAC(alg jwa.SignatureAlgorithm) (*HMACVerifier, error) {
	_, ok := sign.HMACSignFuncs[alg]
	if !ok {
		return nil, errors.Errorf(`unsupported algorithm while trying to create HMAC signer: %s`, alg)
	}
	s, err := sign.New(alg)
	if err != nil {
		return nil, errors.Wrap(err, `failed to generate HMAC signer`)
	}
	return &HMACVerifier{signer: s}, nil
}

func (v HMACVerifier) Verify(payload, signature []byte, key interface{}) (err error) {

	expected, err := v.signer.Sign(payload, key)
	if err != nil {
		return errors.Wrap(err, `failed to generated signature`)
	}

	if !hmac.Equal(signature, expected) {
		return errors.New(`failed to match hmac signature`)
	}
	return nil
}
