//go:generate go run internal/cmd/genheader/main.go

// Package jwk implements JWK as described in https://tools.ietf.org/html/rfc7517
package jwk

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/lestrrat-go/jwx/internal/base64"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/pkg/errors"
)

// GetPublicKey returns the public key based on te private key type.
// For rsa key types *rsa.PublicKey is returned; for ecdsa key types *ecdsa.PublicKey;
// for byte slice (raw) keys, the key itself is returned. If the corresponding
// public key cannot be deduced, an error is returned
func GetPublicKey(key interface{}) (interface{}, error) {
	if key == nil {
		return nil, errors.New(`jwk.New requires a non-nil key`)
	}

	switch v := key.(type) {
	// Mental note: although Public() is defined in both types,
	// you can not coalesce the clauses for rsa.PrivateKey and
	// ecdsa.PrivateKey, as then `v` becomes interface{}
	// b/c the compiler cannot deduce the exact type.
	case *rsa.PrivateKey:
		return v.Public(), nil
	case *ecdsa.PrivateKey:
		return v.Public(), nil
	case []byte:
		return v, nil
	default:
		return nil, errors.Errorf(`invalid key type %T`, key)
	}
}

// New creates a jwk.Key from the given key.
func New(key interface{}) (Key, error) {
	if key == nil {
		return nil, errors.New(`jwk.New requires a non-nil key`)
	}

	switch v := key.(type) {
	case *rsa.PrivateKey:
		return newRSAPrivateKey(v)
	case *rsa.PublicKey:
		return newRSAPublicKey(v)
	case *ecdsa.PrivateKey:
		return newECDSAPrivateKey(v)
	case *ecdsa.PublicKey:
		return newECDSAPublicKey(v)
	case []byte:
		return newSymmetricKey(v)
	default:
		return nil, errors.Errorf(`invalid key type %T`, key)
	}
}

// Fetch fetches a JWK resource specified by a URL
func Fetch(urlstring string, options ...Option) (*Set, error) {
	u, err := url.Parse(urlstring)
	if err != nil {
		return nil, errors.Wrap(err, `failed to parse url`)
	}

	switch u.Scheme {
	case "http", "https":
		return FetchHTTP(urlstring, options...)
	case "file":
		f, err := os.Open(u.Path)
		if err != nil {
			return nil, errors.Wrap(err, `failed to open jwk file`)
		}
		defer f.Close()

		buf, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, errors.Wrap(err, `failed read content from jwk file`)
		}
		return ParseBytes(buf)
	}
	return nil, errors.Errorf(`invalid url scheme %s`, u.Scheme)
}

// FetchHTTP fetches the remote JWK and parses its contents
func FetchHTTP(jwkurl string, options ...Option) (*Set, error) {
	var httpcl HTTPClient = http.DefaultClient
	for _, option := range options {
		switch option.Name() {
		case optkeyHTTPClient:
			httpcl = option.Value().(HTTPClient)
		}
	}

	res, err := httpcl.Get(jwkurl)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch remote JWK")
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch remote JWK (status = %d)", res.StatusCode)
	}

	buf, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read JWK HTTP response body")
	}

	return ParseBytes(buf)
}

func (set *Set) UnmarshalJSON(data []byte) error {
	v, err := ParseBytes(data)
	if err != nil {
		return errors.Wrap(err, `failed to parse jwk.Set`)
	}
	*set = *v
	return nil
}

// Parse parses JWK from the incoming io.Reader.
func Parse(in io.Reader) (*Set, error) {
	m := make(map[string]interface{})
	if err := json.NewDecoder(in).Decode(&m); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal JWK")
	}

	// We must change what the underlying structure that gets decoded
	// out of this JSON is based on parameters within the already parsed
	// JSON (m). In order to do this, we have to go through the tedious
	// task of parsing the contents of this map :/
	if _, ok := m["keys"]; ok {
		var set Set
		if err := set.ExtractMap(m); err != nil {
			return nil, errors.Wrap(err, `failed to extract from map`)
		}
		return &set, nil
	}

	k, err := constructKey(m)
	if err != nil {
		return nil, errors.Wrap(err, `failed to construct key from keys`)
	}
	return &Set{Keys: []Key{k}}, nil
}

// ParseBytes parses JWK from the incoming byte buffer.
func ParseBytes(buf []byte) (*Set, error) {
	return Parse(bytes.NewReader(buf))
}

// ParseString parses JWK from the incoming string.
func ParseString(s string) (*Set, error) {
	return Parse(strings.NewReader(s))
}

// LookupKeyID looks for keys matching the given key id. Note that the
// Set *may* contain multiple keys with the same key id
func (s Set) LookupKeyID(kid string) []Key {
	var keys []Key
	for _, key := range s.Keys {
		if key.KeyID() == kid {
			keys = append(keys, key)
		}
	}
	return keys
}

func (s *Set) ExtractMap(m map[string]interface{}) error {
	raw, ok := m["keys"]
	if !ok {
		return errors.New("missing 'keys' parameter")
	}

	v, ok := raw.([]interface{})
	if !ok {
		return errors.New("invalid 'keys' parameter")
	}

	var ks Set
	for _, c := range v {
		conf, ok := c.(map[string]interface{})
		if !ok {
			return errors.New("invalid element in 'keys'")
		}

		k, err := constructKey(conf)
		if err != nil {
			return errors.Wrap(err, `failed to construct key from map`)
		}
		ks.Keys = append(ks.Keys, k)
	}

	*s = ks
	return nil
}

func constructKey(m map[string]interface{}) (Key, error) {
	kty, ok := m[KeyTypeKey].(string)
	if !ok {
		return nil, errors.Errorf(`unsupported kty type %T`, m[KeyTypeKey])
	}

	var key Key
	switch jwa.KeyType(kty) {
	case jwa.RSA:
		if _, ok := m["d"]; ok {
			key = &RSAPrivateKey{}
		} else {
			key = &RSAPublicKey{}
		}
	case jwa.EC:
		if _, ok := m["d"]; ok {
			key = &ECDSAPrivateKey{}
		} else {
			key = &ECDSAPublicKey{}
		}
	case jwa.OctetSeq:
		key = &SymmetricKey{}
	default:
		return nil, errors.Errorf(`invalid kty %s`, kty)
	}

	if err := key.ExtractMap(m); err != nil {
		return nil, errors.Wrap(err, `failed to extract key from map`)
	}

	return key, nil
}

func getRequiredKey(m map[string]interface{}, key string) ([]byte, error) {
	return getKey(m, key, true)
}

func getOptionalKey(m map[string]interface{}, key string) ([]byte, error) {
	return getKey(m, key, false)
}

func getKey(m map[string]interface{}, key string, required bool) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		if !required {
			return nil, errors.Errorf(`missing parameter '%s'`, key)
		}
		return nil, errors.Errorf(`missing required parameter '%s'`, key)
	}

	vs, ok := v.(string)
	if !ok {
		return nil, errors.Errorf(`invalid type for parameter '%s': %T`, key, v)
	}

	buf, err := base64.DecodeString(vs)
	if err != nil {
		return nil, errors.Wrapf(err, `failed to base64 decode key %s`, key)
	}
	return buf, nil
}
