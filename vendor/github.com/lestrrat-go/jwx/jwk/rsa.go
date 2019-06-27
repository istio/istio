package jwk

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"encoding/json"
	"math/big"

	"github.com/lestrrat-go/jwx/internal/base64"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/pkg/errors"
)

func newRSAPublicKey(key *rsa.PublicKey) (*RSAPublicKey, error) {
	if key == nil {
		return nil, errors.New(`non-nil rsa.PublicKey required`)
	}

	var hdr StandardHeaders
	hdr.Set(KeyTypeKey, jwa.RSA)
	return &RSAPublicKey{
		headers: &hdr,
		key:     key,
	}, nil
}

func newRSAPrivateKey(key *rsa.PrivateKey) (*RSAPrivateKey, error) {
	if key == nil {
		return nil, errors.New(`non-nil rsa.PrivateKey required`)
	}

	if len(key.Primes) < 2 {
		return nil, errors.New("two primes required for RSA private key")
	}

	var hdr StandardHeaders
	hdr.Set(KeyTypeKey, jwa.RSA)
	return &RSAPrivateKey{
		headers: &hdr,
		key:     key,
	}, nil
}

func (k RSAPrivateKey) PublicKey() (*RSAPublicKey, error) {
	return newRSAPublicKey(&k.key.PublicKey)
}

func (k *RSAPublicKey) Materialize() (interface{}, error) {
	if k.key == nil {
		return nil, errors.New(`key has no rsa.PublicKey associated with it`)
	}
	return k.key, nil
}

func (k *RSAPrivateKey) Materialize() (interface{}, error) {
	if k.key == nil {
		return nil, errors.New(`key has no rsa.PrivateKey associated with it`)
	}
	return k.key, nil
}

func (k RSAPublicKey) MarshalJSON() (buf []byte, err error) {

	m := map[string]interface{}{}
	if err := k.PopulateMap(m); err != nil {
		return nil, errors.Wrap(err, `failed to populate public key values`)
	}

	return json.Marshal(m)
}

func (k RSAPublicKey) PopulateMap(m map[string]interface{}) (err error) {

	if err := k.headers.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate header values`)
	}

	m[`n`] = base64.EncodeToString(k.key.N.Bytes())
	m[`e`] = base64.EncodeUint64ToString(uint64(k.key.E))

	return nil
}

func (k *RSAPublicKey) UnmarshalJSON(data []byte) (err error) {

	m := map[string]interface{}{}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, `failed to unmarshal public key`)
	}

	if err := k.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract data from map`)
	}
	return nil
}

func (k *RSAPublicKey) ExtractMap(m map[string]interface{}) (err error) {

	const (
		eKey = `e`
		nKey = `n`
	)

	nbuf, err := getRequiredKey(m, nKey)
	if err != nil {
		return errors.Wrapf(err, `failed to get required key %s`, nKey)
	}
	delete(m, nKey)

	ebuf, err := getRequiredKey(m, eKey)
	if err != nil {
		return errors.Wrapf(err, `failed to get required key %s`, eKey)
	}
	delete(m, eKey)

	var n, e big.Int
	n.SetBytes(nbuf)
	e.SetBytes(ebuf)

	var hdrs StandardHeaders
	if err := hdrs.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract header values`)
	}

	*k = RSAPublicKey{
		headers: &hdrs,
		key:     &rsa.PublicKey{E: int(e.Int64()), N: &n},
	}
	return nil
}

func (k RSAPrivateKey) MarshalJSON() (buf []byte, err error) {

	m := make(map[string]interface{})
	if err := k.PopulateMap(m); err != nil {
		return nil, errors.Wrap(err, `failed to populate private key values`)
	}

	return json.Marshal(m)
}

func (k RSAPrivateKey) PopulateMap(m map[string]interface{}) (err error) {

	const (
		dKey  = `d`
		pKey  = `p`
		qKey  = `q`
		dpKey = `dp`
		dqKey = `dq`
		qiKey = `qi`
	)

	if err := k.headers.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate header values`)
	}

	pubkey, _ := newRSAPublicKey(&k.key.PublicKey)
	if err := pubkey.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate public key values`)
	}

	if err := k.headers.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate header values`)
	}
	m[dKey] = base64.EncodeToString(k.key.D.Bytes())
	m[pKey] = base64.EncodeToString(k.key.Primes[0].Bytes())
	m[qKey] = base64.EncodeToString(k.key.Primes[1].Bytes())
	if v := k.key.Precomputed.Dp; v != nil {
		m[dpKey] = base64.EncodeToString(v.Bytes())
	}
	if v := k.key.Precomputed.Dq; v != nil {
		m[dqKey] = base64.EncodeToString(v.Bytes())
	}
	if v := k.key.Precomputed.Qinv; v != nil {
		m[qiKey] = base64.EncodeToString(v.Bytes())
	}
	return nil
}

func (k *RSAPrivateKey) UnmarshalJSON(data []byte) (err error) {

	m := map[string]interface{}{}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, `failed to unmarshal public key`)
	}

	var key RSAPrivateKey
	if err := key.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract data from map`)
	}
	*k = key

	return nil
}

func (k *RSAPrivateKey) ExtractMap(m map[string]interface{}) (err error) {

	const (
		dKey  = `d`
		pKey  = `p`
		qKey  = `q`
		dpKey = `dp`
		dqKey = `dq`
		qiKey = `qi`
	)

	dbuf, err := getRequiredKey(m, dKey)
	if err != nil {
		return errors.Wrap(err, `failed to get required key`)
	}
	delete(m, dKey)

	pbuf, err := getRequiredKey(m, pKey)
	if err != nil {
		return errors.Wrap(err, `failed to get required key`)
	}
	delete(m, pKey)

	qbuf, err := getRequiredKey(m, qKey)
	if err != nil {
		return errors.Wrap(err, `failed to get required key`)
	}
	delete(m, qKey)

	var d, q, p big.Int
	d.SetBytes(dbuf)
	q.SetBytes(qbuf)
	p.SetBytes(pbuf)

	var dp, dq, qi *big.Int

	dpbuf, err := getOptionalKey(m, dpKey)
	if err == nil {
		delete(m, dpKey)

		dp = &big.Int{}
		dp.SetBytes(dpbuf)
	}

	dqbuf, err := getOptionalKey(m, dqKey)
	if err == nil {
		delete(m, dqKey)

		dq = &big.Int{}
		dq.SetBytes(dqbuf)
	}

	qibuf, err := getOptionalKey(m, qiKey)
	if err == nil {
		delete(m, qiKey)

		qi = &big.Int{}
		qi.SetBytes(qibuf)
	}

	var pubkey RSAPublicKey
	if err := pubkey.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract fields for public key`)
	}

	materialized, err := pubkey.Materialize()
	if err != nil {
		return errors.Wrap(err, `failed to materialize RSA public key`)
	}
	rsaPubkey := materialized.(*rsa.PublicKey)

	var key rsa.PrivateKey
	key.PublicKey = *rsaPubkey
	key.D = &d
	key.Primes = []*big.Int{&p, &q}

	if dp != nil {
		key.Precomputed.Dp = dp
	}
	if dq != nil {
		key.Precomputed.Dq = dq
	}
	if qi != nil {
		key.Precomputed.Qinv = qi
	}

	*k = RSAPrivateKey{
		headers: pubkey.headers,
		key:     &key,
	}
	return nil
}

// Thumbprint returns the JWK thumbprint using the indicated
// hashing algorithm, according to RFC 7638
func (k RSAPrivateKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	return rsaThumbprint(hash, &k.key.PublicKey)
}

func (k RSAPublicKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	return rsaThumbprint(hash, k.key)
}

func rsaThumbprint(hash crypto.Hash, key *rsa.PublicKey) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"e":"`)
	buf.WriteString(base64.EncodeUint64ToString(uint64(key.E)))
	buf.WriteString(`","kty":"RSA","n":"`)
	buf.WriteString(base64.EncodeToString(key.N.Bytes()))
	buf.WriteString(`"}`)

	h := hash.New()
	buf.WriteTo(h)
	return h.Sum(nil), nil
}
