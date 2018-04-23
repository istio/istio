package main

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

var (
	client *http.Client
)

func init() {
	proxyUrl, _ := url.Parse("http://localhost:15002")
	client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
}

// Minimal test to check the standalone server runs with some valid config.
func TestAppend(t *testing.T) {
	res, err := get(t, "http://appendh.test.istio.io/foo")
	if err != nil {
		return
	}
	if !strings.Contains(res, "Istio-Custom-Header=user-defined-value") {
		t.Error("Header not found in ", res)
		return
	}
}

func TestByon(t *testing.T) {
	res, err := get(t, "http://mybyon.test.istio.io/foo")
	if err != nil {
		return
	}
	// The request header will be the original one, from the request, even if the
	// request is sent to byon.test.istio.io
	if !strings.Contains(res, "Host=mybyon.test.istio.io") {
		t.Error("Header not found in ", res)
		return
	}
	t.Log(res)
}

// get returns the body of the request, after making basic checks on the response
func get(t *testing.T, url string) (string, error) {
	res, err := client.Get("http://mybyon.test.istio.io/foo")
	if err != nil {
		t.Error(err)
		return "", err
	}
	resdmp, _ := httputil.DumpResponse(res, true)
	ress := string(resdmp)
	if res.StatusCode != 200 {
		t.Error("Invalid response code ", res.StatusCode)
		return "", fmt.Errorf("Invalid response code %d: %s", res.StatusCode, ress)
	}
	return ress, nil
}
