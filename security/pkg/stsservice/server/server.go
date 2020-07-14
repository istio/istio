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

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"

	"istio.io/istio/security/pkg/stsservice"
	"istio.io/pkg/log"
)

const (
	// TokenPath is url path for handling STS requests.
	TokenPath = "/token"
	// StsStatusPath is the path for dumping STS status.
	StsStatusPath = "/stsStatus"
	// URLEncodedForm is the encoding type specified in a STS request.
	URLEncodedForm = "application/x-www-form-urlencoded"
	// TokenExchangeGrantType is the required value for "grant_type" parameter in a STS request.
	TokenExchangeGrantType = "urn:ietf:params:oauth:grant-type:token-exchange"
	// SubjectTokenType is the required token type in a STS request.
	SubjectTokenType = "urn:ietf:params:oauth:token-type:jwt"
)

var stsServerLog = log.RegisterScope("stsserver", "STS service debugging", 0)

// error code sent in a STS error response. A full list of error code is
// defined in https://tools.ietf.org/html/rfc6749#section-5.2.
const (
	// If the request itself is not valid or if either the "subject_token" or
	// "actor_token" are invalid or unacceptable, the STS server must set
	// error code to "invalid_request".
	invalidRequest = "invalid_request"
	// If the authorization server is unwilling or unable to issue a token, the
	// STS server should set error code to "invalid_target".
	invalidTarget = "invalid_target"
)

// Server watches HTTP requests for security token service (STS), and returns
// token in response.
type Server struct {
	// tokenManager takes STS request parameters and generates tokens, and returns
	// generated token to the STS server.
	tokenManager stsservice.TokenManager
	stsServer    *http.Server
	// Port number that server listens on.
	Port int
}

// Config for the STS server.
type Config struct {
	LocalHostAddr string
	LocalPort     int
}

// NewServer creates a new STS server.
func NewServer(config Config, tokenManager stsservice.TokenManager) (*Server, error) {
	s := &Server{
		tokenManager: tokenManager,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(TokenPath, s.ServeStsRequests)
	mux.HandleFunc(StsStatusPath, s.DumpStsStatus)
	s.stsServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", config.LocalHostAddr, config.LocalPort),
		Handler: mux,
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.LocalHostAddr, config.LocalPort))
	if err != nil {
		log.Errorf("Server failed to listen %v", err)
		return nil, err
	}
	// If passed in port is 0, get the actual chosen port.
	s.Port = ln.Addr().(*net.TCPAddr).Port
	go func() {
		stsServerLog.Infof("Start listening on %s:%d", config.LocalHostAddr, s.Port)
		err := s.stsServer.Serve(ln)
		// ListenAndServe always returns a non-nil error.
		stsServerLog.Errora(err)
	}()
	return s, nil
}

// ServeStsRequests handles STS requests and sends exchanged token in responses.
func (s *Server) ServeStsRequests(w http.ResponseWriter, req *http.Request) {
	reqParam, validationError := s.validateStsRequest(req)
	if validationError != nil {
		stsServerLog.Warnf("STS request is invalid: %v", validationError)
		// If request is invalid, the error code must be "invalid_request".
		// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
		s.sendErrorResponse(w, invalidRequest, validationError)
		return
	}
	tokenDataJSON, genError := s.tokenManager.GenerateToken(reqParam)
	if genError != nil {
		stsServerLog.Warnf("token manager fails to generate token: %v", genError)
		// If the authorization server is unable to issue a token, the "invalid_target" error code
		// should be used in the error response.
		// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.2.2.
		s.sendErrorResponse(w, invalidTarget, genError)
		return
	}
	s.sendSuccessfulResponse(w, tokenDataJSON)
}

// validateStsRequest validates a STS request, and extracts STS parameters from the request.
func (s *Server) validateStsRequest(req *http.Request) (stsservice.StsRequestParameters, error) {
	reqParam := stsservice.StsRequestParameters{}
	if req == nil {
		return reqParam, errors.New("request is nil")
	}

	reqDump, _ := httputil.DumpRequest(req, true)
	stsServerLog.Debugf("Received STS request: %s", string(reqDump))
	if req.Method != "POST" {
		return reqParam, fmt.Errorf("request method is invalid, should be POST but get %s", req.Method)
	}
	if req.Header.Get("Content-Type") != URLEncodedForm {
		return reqParam, fmt.Errorf("request content type is invalid, should be %s but get %s", URLEncodedForm,
			req.Header.Get("Content-type"))
	}
	if parseErr := req.ParseForm(); parseErr != nil {
		return reqParam, fmt.Errorf("failed to parse query from STS request: %v", parseErr)
	}
	if req.PostForm.Get("grant_type") != TokenExchangeGrantType {
		return reqParam, fmt.Errorf("request query grant_type is invalid, should be %s but get %s",
			TokenExchangeGrantType, req.PostForm.Get("grant_type"))
	}
	// Only a JWT token is accepted.
	if req.PostForm.Get("subject_token") == "" {
		return reqParam, errors.New("subject_token is empty")
	}
	if req.PostForm.Get("subject_token_type") != SubjectTokenType {
		return reqParam, fmt.Errorf("subject_token_type is invalid, should be %s but get %s",
			SubjectTokenType, req.PostForm.Get("subject_token_type"))
	}
	reqParam.GrantType = req.PostForm.Get("grant_type")
	reqParam.Resource = req.PostForm.Get("resource")
	reqParam.Audience = req.PostForm.Get("audience")
	reqParam.Scope = req.PostForm.Get("scope")
	reqParam.RequestedTokenType = req.PostForm.Get("requested_token_type")
	reqParam.SubjectToken = req.PostForm.Get("subject_token")
	reqParam.SubjectTokenType = req.PostForm.Get("subject_token_type")
	reqParam.ActorToken = req.PostForm.Get("actor_token")
	reqParam.ActorTokenType = req.PostForm.Get("actor_token_type")
	return reqParam, nil
}

// sendErrorResponse takes error type and error details, generates an error response and sends out.
func (s *Server) sendErrorResponse(w http.ResponseWriter, errorType string, errDetail error) {
	w.Header().Add("Content-Type", "application/json")
	if errorType == invalidRequest {
		w.WriteHeader(http.StatusBadRequest)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	errResp := stsservice.StsErrorResponse{
		Error:            errorType,
		ErrorDescription: errDetail.Error(),
	}
	if errRespJSON, err := json.MarshalIndent(errResp, "", "  "); err == nil {
		if _, err := w.Write(errRespJSON); err != nil {
			stsServerLog.Errorf("failure in sending STS error response (%v): %v", errResp, err)
			return
		}
		stsServerLog.Debugf("sent out STS error response: %v", errResp)
	} else {
		stsServerLog.Errorf("failure in marshaling error response (%v) into JSON: %v", errResp, err)
	}
}

// sendSuccessfulResponse takes token data and generates a successful STS response, and sends out the STS response.
func (s *Server) sendSuccessfulResponse(w http.ResponseWriter, tokenData []byte) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(tokenData); err != nil {
		stsServerLog.Errorf("failure in sending STS success response: %v", err)
	}
	stsServerLog.Debug("sent out STS success response")
}

// DumpStsStatus handles requests for dumping STS status, including STS requests being served,
// tokens being fetched.
func (s *Server) DumpStsStatus(w http.ResponseWriter, req *http.Request) {
	reqDump, _ := httputil.DumpRequest(req, true)
	stsServerLog.Debugf("Received STS request: %s", string(reqDump))

	stsStatusJSON, err := s.tokenManager.DumpTokenStatus()
	if err != nil {
		stsServerLog.Errorf("token manager failed at dumping token status: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		failureMessage := fmt.Sprintf("failure in dumping STS server status: %v", err)
		if _, err := w.Write([]byte(failureMessage)); err != nil {
			stsServerLog.Errorf("failure in sending error response to a STS dump request: %v", err)
		}
		return
	}
	w.Header().Add("Content-Type", "application/json")
	if _, err := w.Write(stsStatusJSON); err != nil {
		stsServerLog.Errorf("failure in sending STS status dump: %v", err)
		return
	}
	stsServerLog.Debug("sent out STS status dump")
}

// Stop closes the server
func (s *Server) Stop() {
	if err := s.stsServer.Shutdown(context.TODO()); err != nil {
		stsServerLog.Errorf("failed to shut down STS server: %v", err)
	}
}
