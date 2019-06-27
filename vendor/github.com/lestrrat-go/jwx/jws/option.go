package jws

import (
	"github.com/lestrrat-go/jwx/internal/option"
	"github.com/lestrrat-go/jwx/jws/sign"
)

type Option = option.Interface

const (
	optkeyPayloadSigner    = `payload-signer`
	optkeyHeaders          = `headers`
)

func WithSigner(signer sign.Signer, key interface{}, public, protected Headers) Option {
	return option.New(optkeyPayloadSigner, &payloadSigner{
		signer:    signer,
		key:       key,
		protected: protected,
		public:    public,
	})
}

func WithHeaders(h Headers) Option {
	return option.New(optkeyHeaders, h)
}
