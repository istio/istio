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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"syscall"

	"istio.io/pkg/log"
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
	// REQUIRED. The value "urn:ietf:params:oauth:grant-type:token- exchange"
	// indicates that a token exchange is being performed.
	grantType            string
	// OPTIONAL. Indicates the location of the target service or resource where
	// the client intends to use the requested security token.
	resource             string
	// OPTIONAL. The logical name of the target service where the client intends
	// to use the requested security token.
	audience             string
	// OPTIONAL. A list of space-delimited, case-sensitive strings, that allow
	// the client to specify the desired scope of the requested security token in the
	// context of the service or resource where the token will be used.
	scope                string
	// OPTIONAL. An identifier, for the type of the requested security token.
	requestedTokenType   string
	// REQUIRED. A security token that represents the identity of the party on
	// behalf of whom the request is being made.
	subjectToken         string
	// REQUIRED. An identifier, that indicates the type of the security token in
	// the "subject_token" parameter.
	subjectTokenType     string
	// OPTIONAL. A security token that represents the identity of the acting party.
	actorToken           string
	// An identifier, that indicates the type of the security token in the
	// "actor_token" parameter.
	actorTokenType       string
}

// StsResponseParameters stores all attributes sent as JSON in a successful STS
// response. These attributes are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.1
type StsResponseParameters struct {
	// REQUIRED. The security token issued by the authorization server
	// in response to the token exchange request.
	accessToken 		    string `json:"access_token"`
	// REQUIRED. An identifier, representation of the issued security token.
	issuedTokenType     string `json:"issued_token_type"`
	// REQUIRED. A case-insensitive value specifying the method of using the access
	// token issued. It provides the client with information about how to utilize the
	// access token to access protected resources.
	tokenType           string `json:"token_type"`
	// RECOMMENDED. The validity lifetime, in seconds, of the token issued by the
	// authorization server.
	expiresIn           string `json:"expires_in"`
	// OPTIONAL, if the scope of the issued security token is identical to the
	// scope requested by the client; otherwise, REQUIRED.
	scope               string `json:"scope"`
	// OPTIONAL. A refresh token will typically not be issued when the exchange is
	// of one temporary credential (the subject_token) for a different temporary
	// credential (the issued token) for use in some other context.
	refreshToken        string `json:"refresh_token"`
}

// StsErrorResponse stores all error parameters sent as JSON in a STS error response.
// The error parameters are defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
type StsErrorResponse struct {
	// REQUIRED. A single ASCII error code.
	error               string `json:"error"`
	// OPTIONAL. Human-readable ASCII [USASCII] text providing additional information.
	errorDescription    string `json:"error_description"`
	// OPTIONAL. A URI identifying a human-readable web page with information
	// about the error.
	errorUri            string `json:"error_uri"`
}

// error code sent in a STS error response. A full list of error code is
// defined in https://tools.ietf.org/html/rfc6749#section-5.2.
const (
	// If the request itself is not valid or if either the "subject_token" or
	// "actor_token" are invalid or unacceptable, the STS server must set
	// error code to "invalid_request".
	invalidRequest       = "invalid_request"
	// If the authorization server is unwilling or unable to issue a token, the
	// STS server should set error code to "invalid_target".
	invalidTarget        = "invalid_target"
)

// TokenManager contains methods for fetching token.
type TokenManager interface {
	// GenerateToken takes STS request parameters and generates token. Returns
	// StsResponseParameters in JSON.
	GenerateToken(parameters StsRequestParameters) ([]byte, error)
	// DumpTokenStatus dumps status of all generated tokens and returns status in JSON.
	DumpTokenStatus() ([]byte, error)
}

// Server watches HTTP requests for security token service (STS), and returns
// token in response.
type Server struct {
	// tokenManager takes STS request parameters and generates tokens, and returns
	// generated token to the STS server.
	tokenManager TokenManager
	stsServer    *http.Server
}

// Config for the STS server.
type Config struct {
	LocalHostAddr string
	LocalPort 	  uint16
}

// NewServer creates a new STS server.
func NewServer(config Config, tokenManager TokenManager) (*Server, error) {
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

// ServeStsRequests handles STS requests and sends exchanged token in responses.
func (s *Server) ServeStsRequests(w http.ResponseWriter, req *http.Request) {
	reqParam, validationError := s.validateStsRequest(req)
	if validationError != nil {
		stsServiceLog.Warnf("STS request is invalid: %s", validationError.Error())
		// If request is invalid, the error code must be "invalid_request".
		// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
		if sendErr := s.sendErrorResponse(w, invalidRequest, validationError); sendErr != nil {
			stsServiceLog.Errorf("failed to write STS error response: %s", sendErr.Error())
		}
		return
	}
	tokenDataJSON, genError := s.tokenManager.GenerateToken(reqParam)
	if genError != nil {
		stsServiceLog.Warnf("token manager fails to generate token: %s", genError.Error())
		// If the authorization server is unable to issue a token, the "invalid_target" error code
		// should be used in the error response.
		// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
		if sendErr := s.sendErrorResponse(w, invalidTarget, genError); sendErr != nil {
			stsServiceLog.Errorf("failed to write STS error response: %s", sendErr.Error())
		}
		return
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

	stsServiceLog.Debugf("Received STS request: %s", httputil.DumpRequest(req, true))
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
	} else {
		stsServiceLog.Errorf("failed to marshal error response into JSON: %s", err.Error())
		return err
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
	stsServiceLog.Debug("Successfully sent out STS response")
	return nil
}

// DumpStsStatus handles requests for dumping STS status, including STS requests being served,
// tokens being fetched.
func (s *Server) DumpStsStatus(w http.ResponseWriter, req *http.Request) {
	stsServiceLog.Debugf("Received STS request: %s", httputil.DumpRequest(req, true))

	stsStatusJSON, err := s.tokenManager.DumpTokenStatus()
	if err != nil {
		stsServiceLog.Errorf("token manager failed to dump token status: %s", err.Error())
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