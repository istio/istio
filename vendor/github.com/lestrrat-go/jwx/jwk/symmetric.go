package jwk

import (
	"crypto"
	"encoding/json"
	"fmt"
	"github.com/lestrrat-go/jwx/internal/base64"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/pkg/errors"
)

func newSymmetricKey(key []byte) (*SymmetricKey, error) {
	if len(key) == 0 {
		return nil, errors.New(`non-empty []byte key required`)
	}

	var hdr StandardHeaders
	hdr.Set(KeyTypeKey, jwa.OctetSeq)
	return &SymmetricKey{
		headers: &hdr,
		key:     key,
	}, nil
}

// Materialize returns the octets for this symmetric key.
// Since this is a symmetric key, this just calls Octets
func (s SymmetricKey) Materialize() (interface{}, error) {
	return s.Octets(), nil
}

// Octets returns the octets in the key
func (s SymmetricKey) Octets() []byte {
	return s.key
}

// Thumbprint returns the JWK thumbprint using the indicated
// hashing algorithm, according to RFC 7638
func (s SymmetricKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	h := hash.New()
	fmt.Fprintf(h, `{"k":"`)
	fmt.Fprintf(h, base64.EncodeToString(s.key))
	fmt.Fprintf(h, `","kty":"oct"}`)
	return h.Sum(nil), nil
}

func (s *SymmetricKey) ExtractMap(m map[string]interface{}) (err error) {

	const kKey = `k`

	kbuf, err := getRequiredKey(m, kKey)
	if err != nil {
		return errors.Wrapf(err, `failed to get required key '%s'`, kKey)
	}
	delete(m, kKey)

	var hdrs StandardHeaders
	if err := hdrs.ExtractMap(m); err != nil {
		return errors.Wrap(err, `failed to extract header values`)
	}

	*s = SymmetricKey{
		headers: &hdrs,
		key:     kbuf,
	}
	return nil
}

func (s SymmetricKey) MarshalJSON() (buf []byte, err error) {

	m := make(map[string]interface{})
	if err := s.PopulateMap(m); err != nil {
		return nil, errors.Wrap(err, `failed to populate symmetric key values`)
	}

	return json.Marshal(m)
}

func (s SymmetricKey) PopulateMap(m map[string]interface{}) (err error) {

	if err := s.headers.PopulateMap(m); err != nil {
		return errors.Wrap(err, `failed to populate header values`)
	}

	const kKey = `k`
	m[kKey] = base64.EncodeToString(s.key)
	return nil
}
