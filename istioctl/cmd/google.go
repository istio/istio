// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func isMCPAddr(addr string) bool {
	// A bit inexact but should be good enough.
	return strings.Contains(addr, ".googleapis.com/") || strings.Contains(addr, ".googleapis.com:443/")
}

func mcpTransport(ctx context.Context, tr http.RoundTripper) (http.RoundTripper, error) {
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("finding default GCP credentials: %w", err)
	}
	return &oauth2.Transport{
		Base:   tr,
		Source: creds.TokenSource,
	}, nil
}
