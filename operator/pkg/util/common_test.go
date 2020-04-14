package util

import (
	"errors"
	"testing"
)

func TestIsFilePath(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want bool
	}{
		{
			desc: "empty",
			in:   "",
			want: false,
		},
		{
			desc: "no-markers",
			in:   "foobar",
			want: false,
		},
		{
			desc: "with-slash",
			in:   "/home/bobby/go_rocks/main",
			want: true,
		},
		{
			desc: "with-period",
			in:   "istio.go",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := IsFilePath(tt.in), tt.want; !(got == want) {
				t.Errorf("%s: got %v, want: %v", tt.desc, got, want)
			}
		})
	}
}

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		desc string
		in   string
		want bool
		err  error
	}{
		{
			desc: "empty",
			in:   "",
			want: false,
			err:  nil,
		},
		{
			desc: "http-standard-url",
			in:   "http://localhost:3000",
			want: true,
			err:  nil,
		},
		{
			desc: "https-standard-url",
			in:   "https://golang.org/",
			want: true,
			err:  nil,
		},
		{
			desc: "ftp-url",
			in:   "ftp://gopher:gopwd@localhost:3000/go",
			want: false,
			err:  nil,
		},
		{
			desc: "tcp-discovery-url",
			in:   "tcp://127.0.0.1:80",
			want: false,
			err:  nil,
		},
		{
			desc: "empty-http",
			in:   "http://",
			want: false,
			err:  errors.New("http:// starts with http but is not a valid URL."),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := IsHTTPURL(tt.in)
			if want, want_err := tt.want, tt.err; !(got == want) || ((err == nil && want_err != nil) || (err != nil && want_err == nil)) {
				t.Errorf("%s: got :%v, wanted output: %v, got error: %v, wanted error: %v", tt.desc, got, want, err, want_err)
			}
		})
	}
}
