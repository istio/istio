// This file is auto-generated. DO NOT EDIT
package jws

import (
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/pkg/errors"
)

const (
	AlgorithmKey              = "alg"
	ContentTypeKey            = "cty"
	CriticalKey               = "crit"
	JWKKey                    = "jwk"
	JWKSetURLKey              = "jku"
	KeyIDKey                  = "kid"
	TypeKey                   = "typ"
	X509CertChainKey          = "x5c"
	X509CertThumbprintKey     = "x5t"
	X509CertThumbprintS256Key = "x5t#S256"
	X509URLKey                = "x5u"
)

type Headers interface {
	Get(string) (interface{}, bool)
	Set(string, interface{}) error
	Algorithm() jwa.SignatureAlgorithm
}

type StandardHeaders struct {
	JWSalgorithm              jwa.SignatureAlgorithm `json:"alg,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.1
	JWScontentType            string                 `json:"cty,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.10
	JWScritical               []string               `json:"crit,omitempty"`     // https://tools.ietf.org/html/rfc7515#section-4.1.11
	JWSjwk                    *jwk.Set               `json:"jwk,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.3
	JWSjwkSetURL              string                 `json:"jku,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.2
	JWSkeyID                  string                 `json:"kid,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.4
	JWStyp                    string                 `json:"typ,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.9
	JWSx509CertChain          []string               `json:"x5c,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.6
	JWSx509CertThumbprint     string                 `json:"x5t,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.7
	JWSx509CertThumbprintS256 string                 `json:"x5t#S256,omitempty"` // https://tools.ietf.org/html/rfc7515#section-4.1.8
	JWSx509URL                string                 `json:"x5u,omitempty"`      // https://tools.ietf.org/html/rfc7515#section-4.1.5
	privateParams             map[string]interface{}
}

func (h *StandardHeaders) Algorithm() jwa.SignatureAlgorithm {
	return h.JWSalgorithm
}

func (h *StandardHeaders) Get(name string) (interface{}, bool) {
	switch name {
	case AlgorithmKey:
		v := h.JWSalgorithm
		if v == "" {
			return nil, false
		}
		return v, true
	case ContentTypeKey:
		v := h.JWScontentType
		if v == "" {
			return nil, false
		}
		return v, true
	case CriticalKey:
		v := h.JWScritical
		if len(v) == 0 {
			return nil, false
		}
		return v, true
	case JWKKey:
		v := h.JWSjwk
		if v == nil {
			return nil, false
		}
		return v, true
	case JWKSetURLKey:
		v := h.JWSjwkSetURL
		if v == "" {
			return nil, false
		}
		return v, true
	case KeyIDKey:
		v := h.JWSkeyID
		if v == "" {
			return nil, false
		}
		return v, true
	case TypeKey:
		v := h.JWStyp
		if v == "" {
			return nil, false
		}
		return v, true
	case X509CertChainKey:
		v := h.JWSx509CertChain
		if len(v) == 0 {
			return nil, false
		}
		return v, true
	case X509CertThumbprintKey:
		v := h.JWSx509CertThumbprint
		if v == "" {
			return nil, false
		}
		return v, true
	case X509CertThumbprintS256Key:
		v := h.JWSx509CertThumbprintS256
		if v == "" {
			return nil, false
		}
		return v, true
	case X509URLKey:
		v := h.JWSx509URL
		if v == "" {
			return nil, false
		}
		return v, true
	default:
		v, ok := h.privateParams[name]
		return v, ok
	}
}

func (h *StandardHeaders) Set(name string, value interface{}) error {
	switch name {
	case AlgorithmKey:
		if err := h.JWSalgorithm.Accept(value); err != nil {
			return errors.Wrapf(err, `invalid value for %s key`, AlgorithmKey)
		}
		return nil
	case ContentTypeKey:
		if v, ok := value.(string); ok {
			h.JWScontentType = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, ContentTypeKey, value)
	case CriticalKey:
		if v, ok := value.([]string); ok {
			h.JWScritical = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, CriticalKey, value)
	case JWKKey:
		v, ok := value.(*jwk.Set)
		if ok {
			h.JWSjwk = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, JWKKey, value)
	case JWKSetURLKey:
		if v, ok := value.(string); ok {
			h.JWSjwkSetURL = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, JWKSetURLKey, value)
	case KeyIDKey:
		if v, ok := value.(string); ok {
			h.JWSkeyID = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, KeyIDKey, value)
	case TypeKey:
		if v, ok := value.(string); ok {
			h.JWStyp = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, TypeKey, value)
	case X509CertChainKey:
		if v, ok := value.([]string); ok {
			h.JWSx509CertChain = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, X509CertChainKey, value)
	case X509CertThumbprintKey:
		if v, ok := value.(string); ok {
			h.JWSx509CertThumbprint = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, X509CertThumbprintKey, value)
	case X509CertThumbprintS256Key:
		if v, ok := value.(string); ok {
			h.JWSx509CertThumbprintS256 = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, X509CertThumbprintS256Key, value)
	case X509URLKey:
		if v, ok := value.(string); ok {
			h.JWSx509URL = v
			return nil
		}
		return errors.Errorf(`invalid value for %s key: %T`, X509URLKey, value)
	default:
		if h.privateParams == nil {
			h.privateParams = map[string]interface{}{}
		}
		h.privateParams[name] = value
	}
	return nil
}
