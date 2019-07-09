package jwk

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/lestrrat-go/jwx/internal/base64"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/pkg/errors"
)

func newECDSAPublicKey(key *ecdsa.PublicKey) (*ECDSAPublicKey, error) {
	if key == nil {
		return nil, errors.New(`non-nil ecdsa.PublicKey required`)
	}

	var hdr StandardHeaders
	hdr.Set(KeyTypeKey, jwa.EC)
	return &ECDSAPublicKey{
		headers: &hdr,
		key:     key,
	}, nil
}

func newECDSAPrivateKey(key *ecdsa.PrivateKey) (*ECDSAPrivateKey, error) {
	if key == nil {
		return nil, errors.New(`non-nil ecdsa.PrivateKey required`)
	}

	var hdr StandardHeaders
	hdr.Set(KeyTypeKey, jwa.EC)
	return &ECDSAPrivateKey{
		headers: &hdr,
		key:     key,
	}, nil
}

func (k ECDSAPrivateKey) PublicKey() (*ECDSAPublicKey, error) {
	return newECDSAPublicKey(&k.key.PublicKey)
}

// Materialize returns the EC-DSA public key represented by this JWK
func (k ECDSAPublicKey) Materialize() (interface{}, error) {
	return k.key, nil
}

func (k ECDSAPublicKey) Curve() jwa.EllipticCurveAlgorithm {
	return jwa.EllipticCurveAlgorithm(k.key.Curve.Params().Name)
}

func (k ECDSAPrivateKey) Curve() jwa.EllipticCurveAlgorithm {
	return jwa.EllipticCurveAlgorithm(k.key.PublicKey.Curve.Params().Name)
}

func ecdsaThumbprint(hash crypto.Hash, crv, x, y string) []byte {
	h := hash.New()
	fmt.Fprintf(h, `{"crv":"`)
	fmt.Fprintf(h, crv)
	fmt.Fprintf(h, `","kty":"EC","x":"`)
	fmt.Fprintf(h, x)
	fmt.Fprintf(h, `","y":"`)
	fmt.Fprintf(h, y)
	fmt.Fprintf(h, `"}`)
	return h.Sum(nil)
}

// Thumbprint returns the JWK thumbprint using the indicated
// hashing algorithm, according to RFC 7638
func (k ECDSAPublicKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	return ecdsaThumbprint(
		hash,
		k.key.Curve.Params().Name,
		base64.EncodeToString(k.key.X.Bytes()),
		base64.EncodeToString(k.key.Y.Bytes()),
	), nil
}

// Thumbprint returns the JWK thumbprint using the indicated
// hashing algorithm, according to RFC 7638
func (k ECDSAPrivateKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	return ecdsaThumbprint(
		hash,
		k.key.Curve.Params().Name,
		base64.EncodeToString(k.key.X.Bytes()),
		base64.EncodeToString(k.key.Y.Bytes()),
	), nil
}

// Materialize returns the EC-DSA private key represented by this JWK
func (k ECDSAPrivateKey) Materialize() (interface{}, error) {
	return k.key, nil
}

func (k ECDSAPublicKey) MarshalJSON() (buf []byte, err error) {

	m := make(map[string]interface{})
	if err := k.PopulateMap(m); err != nil {
		return nil, errors.Wrap(err, `failed to populate public key values`)
	}

	return json.Marshal(m)
}

func (k ECDSAPublicKey) PopulateMap(m map[string]interface{}) (err error) {

	if err := k.headers.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate header values`)
	}

	const (
		xKey   = `x`
		yKey   = `y`
		crvKey = `crv`
	)
	m[xKey] = base64.EncodeToString(k.key.X.Bytes())
	m[yKey] = base64.EncodeToString(k.key.Y.Bytes())
	m[crvKey] = k.key.Curve.Params().Name

	return nil
}

func (k ECDSAPrivateKey) MarshalJSON() (buf []byte, err error) {

	m := make(map[string]interface{})
	if err := k.PopulateMap(m); err != nil {
		return nil, errors.Wrap(err, `failed to populate public key values`)
	}

	return json.Marshal(m)
}

func (k ECDSAPrivateKey) PopulateMap(m map[string]interface{}) (err error) {

	if err := k.headers.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate header values`)
	}

	pubkey, err := newECDSAPublicKey(&k.key.PublicKey)
	if err != nil {
		return errors.Wrap(err, `failed to construct public key from private key`)
	}

	if err := pubkey.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate public key values`)
	}

	m[`d`] = base64.EncodeToString(k.key.D.Bytes())

	return nil
}

func (k *ECDSAPublicKey) UnmarshalJSON(data []byte) (err error) {

	m := map[string]interface{}{}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, `failed to unmarshal public key`)
	}

	if err := k.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract data from map`)
	}
	return nil
}

func (k *ECDSAPublicKey) ExtractMap(m map[string]interface{}) (err error) {

	const (
		xKey   = `x`
		yKey   = `y`
		crvKey = `crv`
	)

	crvname, ok := m[crvKey]
	if !ok {
		return errors.Errorf(`failed to get required key crv`)
	}
	delete(m, crvKey)

	var crv jwa.EllipticCurveAlgorithm
	if err := crv.Accept(crvname); err != nil {
		return errors.Wrap(err, `failed to accept value for crv key`)
	}

	var curve elliptic.Curve
	switch crv {
	case jwa.P256:
		curve = elliptic.P256()
	case jwa.P384:
		curve = elliptic.P384()
	case jwa.P521:
		curve = elliptic.P521()
	default:
		return errors.Errorf(`invalid curve name %s`, crv)
	}

	xbuf, err := getRequiredKey(m, xKey)
	if err != nil {
		return errors.Wrapf(err, `failed to get required key %s`, xKey)
	}
	delete(m, xKey)

	ybuf, err := getRequiredKey(m, yKey)
	if err != nil {
		return errors.Wrapf(err, `failed to get required key %s`, yKey)
	}
	delete(m, yKey)

	var x, y big.Int
	x.SetBytes(xbuf)
	y.SetBytes(ybuf)

	var hdrs StandardHeaders
	if err := hdrs.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract header values`)
	}

	*k = ECDSAPublicKey{
		headers: &hdrs,
		key: &ecdsa.PublicKey{
			Curve: curve,
			X:     &x,
			Y:     &y,
		},
	}
	return nil
}

func (k *ECDSAPrivateKey) UnmarshalJSON(data []byte) (err error) {

	m := map[string]interface{}{}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, `failed to unmarshal public key`)
	}

	if err := k.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract data from map`)
	}
	return nil
}

func (k *ECDSAPrivateKey) ExtractMap(m map[string]interface{}) (err error) {

	const (
		dKey = `d`
	)

	dbuf, err := getRequiredKey(m, dKey)
	if err != nil {
		return errors.Wrapf(err, `failed to get required key %s`, dKey)
	}
	delete(m, dKey)

	var pubkey ECDSAPublicKey
	if err := pubkey.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract public key values`)
	}

	var d big.Int
	d.SetBytes(dbuf)

	*k = ECDSAPrivateKey{
		headers: pubkey.headers,
		key: &ecdsa.PrivateKey{
			PublicKey: *(pubkey.key),
			D:         &d,
		},
	}
	pubkey.headers = nil
	return nil
}
