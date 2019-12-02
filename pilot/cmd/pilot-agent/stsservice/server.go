// Copyright 2019 Istio Authors
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

package stsservice

import (
	"fmt"
	"encoding/json"
	"errors"
	"golang.org/x/net/context"
	"istio.io/pkg/log"
	"net/http"
	"os"
)

const (
	// tokenPath is url path for handling STS requests.
	tokenPath = "/token"
	// stsStatusPath is the path for dumping STS status.
	stsStatusPath = "/stsStatus"
	// urlEncodedForm is the encoding type specified in a STS request.
	urlEncodedForm = "application/x-www-form-urlencoded"
	// tokenExchangeGrantType is the required value for "grant_type" parameter in a STS request.
	tokenExchangeGrantType = "urn:ietf:params:oauth:grant-type:token-exchange"
)

var stsServiceLog = log.RegisterScope("stsServiceLog", "STS service debugging", 0)

// StsRequestParameters stores all STS request attributes defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.1
type StsRequestParameters struct {
	grantType            string  // Required
	resource             string
	audience             string
	scope                string
	requestedTokenType   string
	subjectToken         string  // Required
	subjectTokenType     string  // Required
	actorToken           string
	actorTokenType       string
}

// StsResponseParameters stores all attributes sent as JSON in a successful STS
// response. These attributes are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.1
type StsResponseParameters struct {
	accessToken 		string `json:"access_token"`      // Required
	issuedTokenType     string `json:"issued_token_type"` // Required
	tokenType           string `json:"token_type"`        // Required
	expiresIn           string `json:"expires_in"`
	scope               string `json:"scope"`
    refreshToken        string `json:"refresh_token"`
}

// StsErrorResponse stores all error parameters sent as JSON in a STS error response.
// The error parameters are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2
type StsErrorResponse struct {
    error               string `json:"error"`
	errorDescription    string `json:"error_description"`
	errorUri            string `json:"error_uri"`
}

// error code sent in a STS error response.
// https://tools.ietf.org/html/rfc6749#section-5.2
const (
	invalidRequest       = "invalid_request"
	invalidTarget        = "invalid_target"
)

// TokenManager contains methods for fetching token.
type TokenManager interface {
	// GenerateToken takes STS request parameters and fetches token, returns StsResponseParameters in JSON.
	GenerateToken(attributes StsRequestParameters) ([]byte, error)
	// DumpTokenStatus dumps all token status in JSON
	DumpTokenStatus() ([]byte, error)
}

// Server provides an endpoint for handling security token service (STS) requests.
type Server struct {
	tokenManager *TokenManager
	stsServer    *http.Server
}

// Config for the STS server.
type Config struct {
	LocalHostAddr string
	LocalPort 	  int
}

// ServeStsRequests handles STS requests and sends exchanged token in responses.
func (s *Server) ServeStsRequests(w http.ResponseWriter, req *http.Request) {
	reqParam, validationError := s.validateStsRequest(req)
	if validationError != nil {
		stsServiceLog.Warnf("failed to validate STS request: %s", validationError.Error())
		// If request is invalid, the value of the "error" parameter in the error response must be
		// "invalid_request".
		// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
		if sendErr := s.sendErrorResponse(w, invalidRequest, validationError); sendErr != nil {
			stsServiceLog.Errorf("failed to write STS error response: %s", sendErr.Error())
		}
	}
	tokenDataJSON, fetchError := s.tokenManager.GenerateToken(reqParam)
	if fetchError != nil {
		stsServiceLog.Warnf("failed to exchange token for STS request: %s", fetchError.Error())
		// If the authorization server is unable to issue a token, the "invalid_target" error code
		// should be used in the error response.
		// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
		if sendErr := s.sendErrorResponse(w, invalidTarget, fetchError); sendErr != nil {
			stsServiceLog.Errorf("failed to write STS error response: %s", sendErr.Error())
		}
	}
	if sendErr := s.sendSuccessfulResponse(w, tokenDataJSON); sendErr != nil {
		stsServiceLog.Errorf("failed to write STS successful response: %s", sendErr.Error())
	}
}

// validateStsRequest validates a STS request, and extracts STS parameters from the request.
func (s *Server) validateStsRequest(req *http.Request) (StsRequestParameters, error) {
	reqParam := StsRequestParameters{}
	if req == nil {
		return reqParam, errors.New("request is nil")
	}
	if req.Method != "POST" {
		return reqParam, fmt.Errorf("request method should be POST but get %s", req.Method)
	}
	if req.Header.Get("Content-Type") != urlEncodedForm {
		return reqParam, fmt.Errorf("request content type should be %s but get %s", urlEncodedForm,
			req.Header.Get("Content-type"))
	}
	if parseErr := req.ParseForm(); parseErr != nil {
		return reqParam, fmt.Errorf("failed to parse query from STS request: %s", parseErr.Error())
	}
	if req.PostForm.Get("grant_type") != tokenExchangeGrantType {
		return reqParam, fmt.Errorf("request query grant_type should be %s but get %s",
			tokenExchangeGrantType, req.PostForm.Get("grant_type"))
	}
	if req.PostForm.Get("subject_token") == "" || req.PostForm.Get("subject_token_type") == "" {
		return reqParam, errors.New("request query does not have subject_token or subject_token_type")
	}
	reqParam.grantType          = req.PostForm.Get("grant_type")
	reqParam.resource           = req.PostForm.Get("resource")
	reqParam.audience           = req.PostForm.Get("audience")
	reqParam.scope              = req.PostForm.Get("scope")
	reqParam.requestedTokenType = req.PostForm.Get("requested_token_type")
	reqParam.subjectToken       = req.PostForm.Get("subject_token")
	reqParam.subjectTokenType   = req.PostForm.Get("subject_token_type")
	reqParam.actorToken         = req.PostForm.Get("actor_token")
	reqParam.actorTokenType     = req.PostForm.Get("actor_token_type")
	return reqParam, nil
}

// sendErrorResponse takes error type and error details, generates an error response and sends out.
func (s *Server) sendErrorResponse(w http.ResponseWriter, errorType string, errDetail error) error {
	w.Header().Add("Content-Type", "application/json")
	if errorType == invalidRequest {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	errResp := StsErrorResponse{
		error: errorType,
		errorDescription: errDetail.Error(),
	}
	if errRespJSON, err := json.MarshalIndent(errResp, "", "  "); err == nil {
		if _, err := w.Write(errRespJSON); err != nil {
			return err
		}
	}
	return nil
}

// sendSuccessfulResponse takes token data and generates a successful STS response, and sends out the STS response.
func (s *Server) sendSuccessfulResponse(w http.ResponseWriter, tokenData []byte) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(tokenData); err != nil {
		return err
	}
	return nil
}

// DumpStsStatus handles requests for dumping STS status, including STS requests being served,
// tokens being fetched.
func (s *Server) DumpStsStatus(w http.ResponseWriter, _ *http.Request) {
	stsStatusJSON, err := s.tokenManager.DumpTokenStatus()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		failureMessage := fmt.Sprintf("failed to dump STS server status: %s", err)
		if _, err := w.Write([]byte(failureMessage)); err != nil {
			stsServiceLog.Errorf("failed to write error response: %s", err)
		}
		return
	}
	if _, err := w.Write(stsStatusJSON); err != nil {
		stsServiceLog.Errorf("failed to write STS response: %s", err)
	}
}

func (s *Server) Stop() {
	if err := s.stsServer.Shutdown(context.TODO()); err != nil {
		stsServiceLog.Error("failed to shutdown STS server")
	}
}

// NewServer creates a new status server.
func NewServer(config Config, tokenManager *TokenManager) (*Server, error) {
	s := &Server{
		tokenManager: tokenManager,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(tokenPath, s.ServeStsRequests)
	mux.HandleFunc(stsStatusPath, s.DumpStsStatus)
	s.stsServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", config.LocalHostAddr, config.LocalPort),
		Handler: mux,
	}
	go func() {
		err := s.stsServer.ListenAndServe()
		// ListenAndServe always returns a non-nil error.
		stsServiceLog.Errora(err)
		notifyExit()
	}()
	return s, nil
}

// notifyExit sends SIGTERM to itself
func notifyExit() {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		log.Errora(err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		stsServiceLog.Errorf("failed to send SIGTERM to self: %v", err)
	}
}
