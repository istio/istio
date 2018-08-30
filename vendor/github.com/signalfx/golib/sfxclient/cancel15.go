// +build go1.5

package sfxclient

import (
	"net/http"

	"context"
)

func (h *HTTPSink) withCancel(ctx context.Context, req *http.Request) (err error) {
	req.Cancel = ctx.Done()
	return h.handleResponse(h.Client.Do(req))
}
