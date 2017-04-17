// Copyright 2017 Istio Authors
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

package config

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"

	restful "github.com/emicklei/go-restful"
	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/config/descriptor"
	pb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

type validateFunc func(cfg map[string]string) (rt *Validated, desc descriptor.Finder, ce *adapter.ConfigErrors)

type readBodyFunc func(r io.Reader) ([]byte, error)

// API defines and implements the configuration API.
// The server constructs and uses a validator for validations
// The server uses KeyValueStore to persist keys.
type API struct {
	version  string
	rootPath string

	// used at the back end for validation and storage
	store    KeyValueStore
	validate validateFunc

	// house keeping
	handler http.Handler
	server  *http.Server

	// fault injection
	readBody readBodyFunc
}

// MsgOk defines the text of the OK message in rpc.Status.Message.
const msgOk = "ok"

// APIResponse defines the shape of the api response.
type APIResponse struct {
	Data   interface{} `json:"data,omitempty"`
	Status rpc.Status  `json:"status,omitempty"`
}

// register routes
func (a *API) register(c *restful.Container) {
	ws := &restful.WebService{}
	ws.Consumes(restful.MIME_JSON, "application/yaml", "application/x-yaml")
	ws.Produces(restful.MIME_JSON)
	ws.Path(a.rootPath)

	ws.Route(ws.
		GET("/scopes/{scope}/subjects/{subject}/rules").
		To(a.getRules).
		Doc("Gets rules associated with the given scope and subject").
		Param(ws.PathParameter("scope", "scope").DataType("string")).
		Param(ws.PathParameter("subject", "subject").DataType("string")).
		Writes(APIResponse{}))

	ws.Route(ws.
		PUT("/scopes/{scope}/subjects/{subject}/rules").
		To(a.putRules).
		Doc("Replaces rules associated with the given scope and subject").
		Param(ws.PathParameter("scope", "scope").DataType("string")).
		Param(ws.PathParameter("subject", "subject").DataType("string")).
		Reads(&pb.ServiceConfig{}).
		Writes(APIResponse{}))
	c.Add(ws)
}

// NewAPI creates a new API server
func NewAPI(version string, port uint16, eval expr.Validator, aspectFinder AspectValidatorFinder,
	builderFinder BuilderValidatorFinder, findAspects AdapterToAspectMapper, store KeyValueStore) *API {
	c := restful.NewContainer()
	a := &API{
		version:  version,
		rootPath: fmt.Sprintf("/api/%s", version),
		store:    store,
		readBody: ioutil.ReadAll,
		validate: func(cfg map[string]string) (*Validated, descriptor.Finder, *adapter.ConfigErrors) {
			v := newValidator(aspectFinder, builderFinder, findAspects, true, eval)
			rt, ce := v.validate(cfg)
			return rt, v.descriptorFinder, ce
		},
	}
	a.register(c)
	a.server = &http.Server{Addr: ":" + strconv.Itoa(int(port)), Handler: c}
	a.handler = c
	// ensure that we always send back an APIResponse object.
	c.ServiceErrorHandler(func(err restful.ServiceError, req *restful.Request, resp *restful.Response) {
		writeErrorResponse(err.Code, err.Message, resp)
	})
	return a
}

// Run calls listen and serve on the API server
func (a *API) Run() {
	glog.Warning(a.server.ListenAndServe())
}

// getRules returns the rules document for the scope and the subject.
// "/scopes/{scope}/subjects/{subject}/rules"
func (a *API) getRules(req *restful.Request, resp *restful.Response) {
	funcPath := req.Request.URL.Path[len(a.rootPath):]
	st, msg, data := getRules(a.store, funcPath)
	writeResponse(st, msg, data, resp)
}

func getRules(store KeyValueStore, path string) (statusCode int, msg string, data *pb.ServiceConfig) {
	var val string
	var found bool

	if val, _, found = store.Get(path); !found {
		return http.StatusNotFound, fmt.Sprintf("no rules for %s", path), nil
	}

	m := &pb.ServiceConfig{}
	if err := yaml.Unmarshal([]byte(val), m); err != nil {
		msg := fmt.Sprintf("unable to parse rules at '%s': %v", path, err)
		glog.Warning(msg)
		return http.StatusInternalServerError, msg, nil
	}
	return http.StatusOK, msgOk, m
}

// putRules replaces the entire rules document for the scope and subject
// "/scopes/{scope}/subjects/{subject}/rules"
func (a *API) putRules(req *restful.Request, resp *restful.Response) {

	key := req.Request.URL.Path[len(a.rootPath):]
	var data map[string]string
	var err error
	// TODO optimize only read descriptors and adapters
	if data, _, _, err = readdb(a.store, "/"); err != nil {
		writeErrorResponse(http.StatusInternalServerError, err.Error(), resp)
		return
	}
	// TODO send index back to the client

	var bval []byte
	if bval, err = a.readBody(req.Request.Body); err != nil {
		writeErrorResponse(http.StatusInternalServerError, err.Error(), resp)
		return
	}
	val := string(bval)
	data[key] = val
	/*
		rt *Validated, desc descriptor.Finder, ce *adapter.ConfigErrors
	*/
	var vd *Validated
	var cerr *adapter.ConfigErrors
	if vd, _, cerr = a.validate(data); cerr != nil {
		glog.Warningf("Validation failed with %s\n %s", cerr.Error(), val)
		writeErrorResponse(http.StatusPreconditionFailed, cerr.Error(), resp)
		return
	}

	if _, err = a.store.Set(key, val); err != nil {
		writeErrorResponse(http.StatusInternalServerError, err.Error(), resp)
		return
	}
	// TODO send index back to the client
	writeResponse(http.StatusOK, fmt.Sprintf("Created %s", key),
		vd.rule[*parseRulesKey(key)], resp)
}

// a subset of restful.Response
type response interface {
	// WriteHeaderAndJson is a convenience method for writing the status and a value in Json with a given Content-Type.
	WriteHeaderAndJson(status int, value interface{}, contentType string) error
}

func writeResponse(httpStatus int, msg string, data interface{}, resp response) {
	if err := resp.WriteHeaderAndJson(
		httpStatus,
		&APIResponse{
			Data: data,
			Status: status.WithMessage(
				httpStatusToRPC(httpStatus), msg),
		},
		restful.MIME_JSON,
	); err != nil {
		glog.Warning(err)
	}
}

func writeErrorResponse(httpStatus int, msg string, resp response) {
	writeResponse(httpStatus, msg, nil, resp)
}

func httpStatusToRPC(httpStatus int) (code rpc.Code) {
	var ok bool
	if code, ok = httpStatusToRPCMap[httpStatus]; !ok {
		code = rpc.UNKNOWN
	}
	return code
}

// httpStatusToRpc limited mapping from proto documentation.
var httpStatusToRPCMap = map[int]rpc.Code{
	http.StatusOK:                 rpc.OK,
	http.StatusNotFound:           rpc.NOT_FOUND,
	http.StatusConflict:           rpc.ALREADY_EXISTS,
	http.StatusForbidden:          rpc.PERMISSION_DENIED,
	http.StatusUnauthorized:       rpc.UNAUTHENTICATED,
	http.StatusPreconditionFailed: rpc.FAILED_PRECONDITION,
}
