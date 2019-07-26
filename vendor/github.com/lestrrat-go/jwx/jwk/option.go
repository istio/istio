package jwk

import (
	"net/http"

	"github.com/lestrrat-go/jwx/internal/option"
)

type Option = option.Interface

const (
	optkeyHTTPClient = `http-client`
)

type HTTPClient interface {
	Get(string) (*http.Response, error)
}

func WithHTTPClient(cl HTTPClient) Option {
	return option.New(optkeyHTTPClient, cl)
}
