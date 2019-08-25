package api

import (
	"fmt"
	"strings"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

const (
	ErrOutputStringRequest = "output a string, please"
)

var (
	LastOutputStringError *OutputStringError
)

type OutputStringError struct {
	*retryablehttp.Request
	parsingError     error
	parsedCurlString string
}

func (d *OutputStringError) Error() string {
	if d.parsedCurlString == "" {
		d.parseRequest()
		if d.parsingError != nil {
			return d.parsingError.Error()
		}
	}

	return ErrOutputStringRequest
}

func (d *OutputStringError) parseRequest() {
	body, err := d.Request.BodyBytes()
	if err != nil {
		d.parsingError = err
		return
	}

	// Build cURL string
	d.parsedCurlString = "curl "
	if d.Request.Method != "GET" {
		d.parsedCurlString = fmt.Sprintf("%s-X %s ", d.parsedCurlString, d.Request.Method)
	}
	for k, v := range d.Request.Header {
		for _, h := range v {
			if strings.ToLower(k) == "x-vault-token" {
				h = `$(vault print token)`
			}
			d.parsedCurlString = fmt.Sprintf("%s-H \"%s: %s\" ", d.parsedCurlString, k, h)
		}
	}

	if len(body) > 0 {
		// We need to escape single quotes since that's what we're using to
		// quote the body
		escapedBody := strings.Replace(string(body), "'", "'\"'\"'", -1)
		d.parsedCurlString = fmt.Sprintf("%s-d '%s' ", d.parsedCurlString, escapedBody)
	}

	d.parsedCurlString = fmt.Sprintf("%s%s", d.parsedCurlString, d.Request.URL.String())
}

func (d *OutputStringError) CurlString() string {
	if d.parsedCurlString == "" {
		d.parseRequest()
	}
	return d.parsedCurlString
}
