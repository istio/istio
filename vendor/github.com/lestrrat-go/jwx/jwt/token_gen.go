// This file is auto-generated. DO NOT EDIT
package jwt

import (
	"bytes"
	"encoding/json"
	"github.com/lestrrat-go/jwx/jwt/internal/types"
	"github.com/pkg/errors"
	"time"
)

// Key names for standard claims
const (
	AudienceKey   = "aud"
	ExpirationKey = "exp"
	IssuedAtKey   = "iat"
	IssuerKey     = "iss"
	JwtIDKey      = "jti"
	NotBeforeKey  = "nbf"
	SubjectKey    = "sub"
)

// Token represents a JWT token. The object has convenience accessors
// to 7 standard claims including "aud", "exp", "iat", "iss", "jti", "nbf" and "sub"
// which are type-aware (to an extent). Other claims may be accessed via the `Get`/`Set`
// methods but their types are not taken into consideration at all. If you have non-standard
// claims that you must frequently access, consider wrapping the token in a wrapper
// by embedding the jwt.Token type in it
type Token struct {
	audience      StringList             `json:"aud,omitempty"` // https://tools.ietf.org/html/rfc7519#section-4.1.3
	expiration    *types.NumericDate     `json:"exp,omitempty"` // https://tools.ietf.org/html/rfc7519#section-4.1.4
	issuedAt      *types.NumericDate     `json:"iat,omitempty"` // https://tools.ietf.org/html/rfc7519#section-4.1.6
	issuer        *string                `json:"iss,omitempty"` // https://tools.ietf.org/html/rfc7519#section-4.1.1
	jwtID         *string                `json:"jti,omitempty"` // https://tools.ietf.org/html/rfc7519#section-4.1.7
	notBefore     *types.NumericDate     `json:"nbf,omitempty"` // https://tools.ietf.org/html/rfc7519#section-4.1.5
	subject       *string                `json:"sub,omitempty"` // https://tools.ietf.org/html/rfc7519#section-4.1.2
	privateClaims map[string]interface{} `json:"-"`
}

func (t *Token) Get(s string) (interface{}, bool) {
	switch s {
	case AudienceKey:
		if len(t.audience) == 0 {
			return nil, false
		}
		return []string(t.audience), true
	case ExpirationKey:
		if t.expiration == nil {
			return nil, false
		} else {
			return t.expiration.Get(), true
		}
	case IssuedAtKey:
		if t.issuedAt == nil {
			return nil, false
		} else {
			return t.issuedAt.Get(), true
		}
	case IssuerKey:
		if t.issuer == nil {
			return nil, false
		} else {
			return *(t.issuer), true
		}
	case JwtIDKey:
		if t.jwtID == nil {
			return nil, false
		} else {
			return *(t.jwtID), true
		}
	case NotBeforeKey:
		if t.notBefore == nil {
			return nil, false
		} else {
			return t.notBefore.Get(), true
		}
	case SubjectKey:
		if t.subject == nil {
			return nil, false
		} else {
			return *(t.subject), true
		}
	}
	if v, ok := t.privateClaims[s]; ok {
		return v, true
	}
	return nil, false
}

func (t *Token) Set(name string, v interface{}) error {
	switch name {
	case AudienceKey:
		var x StringList
		if err := x.Accept(v); err != nil {
			return errors.Wrap(err, `invalid value for 'audience' key`)
		}
		t.audience = x
	case ExpirationKey:
		var x types.NumericDate
		if err := x.Accept(v); err != nil {
			return errors.Wrap(err, `invalid value for 'expiration' key`)
		}
		t.expiration = &x
	case IssuedAtKey:
		var x types.NumericDate
		if err := x.Accept(v); err != nil {
			return errors.Wrap(err, `invalid value for 'issuedAt' key`)
		}
		t.issuedAt = &x
	case IssuerKey:
		x, ok := v.(string)
		if !ok {
			return errors.Errorf(`invalid type for 'issuer' key: %T`, v)
		}
		t.issuer = &x
	case JwtIDKey:
		x, ok := v.(string)
		if !ok {
			return errors.Errorf(`invalid type for 'jwtID' key: %T`, v)
		}
		t.jwtID = &x
	case NotBeforeKey:
		var x types.NumericDate
		if err := x.Accept(v); err != nil {
			return errors.Wrap(err, `invalid value for 'notBefore' key`)
		}
		t.notBefore = &x
	case SubjectKey:
		x, ok := v.(string)
		if !ok {
			return errors.Errorf(`invalid type for 'subject' key: %T`, v)
		}
		t.subject = &x
	default:
		if t.privateClaims == nil {
			t.privateClaims = make(map[string]interface{})
		}
		t.privateClaims[name] = v
	}
	return nil
}

func (t Token) Audience() StringList {
	if v, ok := t.Get(AudienceKey); ok {
		return v.([]string)
	}
	return nil
}

func (t Token) Expiration() time.Time {
	if v, ok := t.Get(ExpirationKey); ok {
		return v.(time.Time)
	}
	return time.Time{}
}

func (t Token) IssuedAt() time.Time {
	if v, ok := t.Get(IssuedAtKey); ok {
		return v.(time.Time)
	}
	return time.Time{}
}

// Issuer is a convenience function to retrieve the corresponding value store in the token
// if there is a problem retrieving the value, the zero value is returned. If you need to differentiate between existing/non-existing values, use `Get` instead

func (t Token) Issuer() string {
	if v, ok := t.Get(IssuerKey); ok {
		return v.(string)
	}
	return ""
}

// JwtID is a convenience function to retrieve the corresponding value store in the token
// if there is a problem retrieving the value, the zero value is returned. If you need to differentiate between existing/non-existing values, use `Get` instead

func (t Token) JwtID() string {
	if v, ok := t.Get(JwtIDKey); ok {
		return v.(string)
	}
	return ""
}

func (t Token) NotBefore() time.Time {
	if v, ok := t.Get(NotBeforeKey); ok {
		return v.(time.Time)
	}
	return time.Time{}
}

// Subject is a convenience function to retrieve the corresponding value store in the token
// if there is a problem retrieving the value, the zero value is returned. If you need to differentiate between existing/non-existing values, use `Get` instead

func (t Token) Subject() string {
	if v, ok := t.Get(SubjectKey); ok {
		return v.(string)
	}
	return ""
}

// this is almost identical to json.Encoder.Encode(), but we use Marshal
// to avoid having to remove the trailing newline for each successive
// call to Encode()
func writeJSON(buf *bytes.Buffer, v interface{}, keyName string) error {
	if enc, err := json.Marshal(v); err != nil {
		return errors.Wrapf(err, `failed to encode '%s'`, keyName)
	} else {
		buf.Write(enc)
	}
	return nil
}

// MarshalJSON serializes the token in JSON format. This exists to
// allow flattening of private claims.
func (t Token) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteRune('{')
	if len(t.audience) > 0 {
		buf.WriteRune('"')
		buf.WriteString(AudienceKey)
		buf.WriteString(`":`)
		if err := writeJSON(&buf, t.audience, AudienceKey); err != nil {
			return nil, err
		}
	}
	if t.expiration != nil {
		if buf.Len() > 1 {
			buf.WriteRune(',')
		}
		buf.WriteRune('"')
		buf.WriteString(ExpirationKey)
		buf.WriteString(`":`)
		if err := writeJSON(&buf, t.expiration, ExpirationKey); err != nil {
			return nil, err
		}
	}
	if t.issuedAt != nil {
		if buf.Len() > 1 {
			buf.WriteRune(',')
		}
		buf.WriteRune('"')
		buf.WriteString(IssuedAtKey)
		buf.WriteString(`":`)
		if err := writeJSON(&buf, t.issuedAt, IssuedAtKey); err != nil {
			return nil, err
		}
	}
	if t.issuer != nil {
		if buf.Len() > 1 {
			buf.WriteRune(',')
		}
		buf.WriteRune('"')
		buf.WriteString(IssuerKey)
		buf.WriteString(`":`)
		if err := writeJSON(&buf, t.issuer, IssuerKey); err != nil {
			return nil, err
		}
	}
	if t.jwtID != nil {
		if buf.Len() > 1 {
			buf.WriteRune(',')
		}
		buf.WriteRune('"')
		buf.WriteString(JwtIDKey)
		buf.WriteString(`":`)
		if err := writeJSON(&buf, t.jwtID, JwtIDKey); err != nil {
			return nil, err
		}
	}
	if t.notBefore != nil {
		if buf.Len() > 1 {
			buf.WriteRune(',')
		}
		buf.WriteRune('"')
		buf.WriteString(NotBeforeKey)
		buf.WriteString(`":`)
		if err := writeJSON(&buf, t.notBefore, NotBeforeKey); err != nil {
			return nil, err
		}
	}
	if t.subject != nil {
		if buf.Len() > 1 {
			buf.WriteRune(',')
		}
		buf.WriteRune('"')
		buf.WriteString(SubjectKey)
		buf.WriteString(`":`)
		if err := writeJSON(&buf, t.subject, SubjectKey); err != nil {
			return nil, err
		}
	}
	if len(t.privateClaims) == 0 {
		buf.WriteRune('}')
		return buf.Bytes(), nil
	}
	// If private claims exist, they need to flattened and included in the token
	pcjson, err := json.Marshal(t.privateClaims)
	if err != nil {
		return nil, errors.Wrap(err, `failed to marshal private claims`)
	}
	// remove '{' from the private claims
	pcjson = pcjson[1:]
	if buf.Len() > 1 {
		buf.WriteRune(',')
	}
	buf.Write(pcjson)
	return buf.Bytes(), nil
}

// UnmarshalJSON deserializes data from a JSON data buffer into a Token
func (t *Token) UnmarshalJSON(data []byte) error {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, `failed to unmarshal token`)
	}
	for name, value := range m {
		if err := t.Set(name, value); err != nil {
			return errors.Wrapf(err, `failed to set value for %s`, name)
		}
	}
	return nil
}
