package jwk

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"errors"
)

// KeyUsageType is used to denote what this key should be used for
type KeyUsageType string

const (
	// ForSignature is the value used in the headers to indicate that
	// this key should be used for signatures
	ForSignature KeyUsageType = "sig"
	// ForEncryption is the value used in the headers to indicate that
	// this key should be used for encryptiong
	ForEncryption KeyUsageType = "enc"
)

type CertificateChain struct {
	certs []*x509.Certificate
}

// Errors related to JWK
var (
	ErrInvalidHeaderName  = errors.New("invalid header name")
	ErrInvalidHeaderValue = errors.New("invalid value for header key")
	ErrUnsupportedKty     = errors.New("unsupported kty")
	ErrUnsupportedCurve   = errors.New("unsupported curve")
)

type KeyOperation string
type KeyOperationList []KeyOperation

const (
	KeyOpSign       KeyOperation = "sign"       // (compute digital signature or MAC)
	KeyOpVerify                  = "verify"     // (verify digital signature or MAC)
	KeyOpEncrypt                 = "encrypt"    // (encrypt content)
	KeyOpDecrypt                 = "decrypt"    // (decrypt content and validate decryption, if applicable)
	KeyOpWrapKey                 = "wrapKey"    // (encrypt key)
	KeyOpUnwrapKey               = "unwrapKey"  // (decrypt key and validate decryption, if applicable)
	KeyOpDeriveKey               = "deriveKey"  // (derive key)
	KeyOpDeriveBits              = "deriveBits" // (derive bits not to be used as a key)
)

// Set is a convenience struct to allow generating and parsing
// JWK sets as opposed to single JWKs
type Set struct {
	Keys []Key `json:"keys"`
}

// Key defines the minimal interface for each of the
// key types. Their use and implementation differ significantly
// between each key types, so you should use type assertions
// to perform more specific tasks with each key
type Key interface {
	Headers

	// Materialize creates the corresponding key. For example,
	// RSA types would create *rsa.PublicKey or *rsa.PrivateKey,
	// EC types would create *ecdsa.PublicKey or *ecdsa.PrivateKey,
	// and OctetSeq types create a []byte key.
	Materialize() (interface{}, error)

	// Thumbprint returns the JWK thumbprint using the indicated
	// hashing algorithm, according to RFC 7638
	Thumbprint(crypto.Hash) ([]byte, error)
}

type headers interface {
	Headers
}

// RSAPublicKey is a type of JWK generated from RSA public keys
type RSAPublicKey struct {
	headers
	key *rsa.PublicKey
}

// RSAPrivateKey is a type of JWK generated from RSA private keys
type RSAPrivateKey struct {
	headers
	key *rsa.PrivateKey
}

// SymmetricKey is a type of JWK generated from symmetric keys
type SymmetricKey struct {
	headers
	key []byte
}

// ECDSAPublicKey is a type of JWK generated from ECDSA public keys
type ECDSAPublicKey struct {
	headers
	key *ecdsa.PublicKey
}

// ECDSAPrivateKey is a type of JWK generated from ECDH-ES private keys
type ECDSAPrivateKey struct {
	headers
	key *ecdsa.PrivateKey
}
