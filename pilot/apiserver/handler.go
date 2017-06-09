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

package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"istio.io/pilot/cmd/version"
	"istio.io/pilot/model"

	restful "github.com/emicklei/go-restful"
	"github.com/golang/glog"
)

// Status returns 200 to indicate healthy
// Could be expanded later to identify the health of downstream dependencies such as kube, etc.
func (api *API) Status(request *restful.Request, response *restful.Response) {
	response.WriteHeader(http.StatusOK)
}

// GetConfig retrieves the config object from the configuration registry
func (api *API) GetConfig(request *restful.Request, response *restful.Response) {

	params := request.PathParameters()
	k, err := setup(params)
	if err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	glog.V(2).Infof("Getting config from Istio registry: %+v", k)
	// TODO: incorrect use with new registry
	proto, ok, _ := api.registry.Get(k.Kind, k.Name)
	if !ok {
		errLocal := &model.ItemNotFoundError{Key: k.Name}
		api.writeError(http.StatusNotFound, errLocal.Error(), response)
		return
	}

	var schema model.ProtoSchema
	retrieved, err := schema.ToJSON(proto)
	if err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
		return
	}
	var retJSON interface{}
	if err = json.Unmarshal([]byte(retrieved), &retJSON); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
		return
	}
	config := Config{
		Name: params["name"],
		Type: params["kind"],
		Spec: retJSON,
	}
	glog.V(2).Infof("Retrieved config %+v", config)
	if err = response.WriteHeaderAndEntity(http.StatusOK, config); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

// AddConfig creates and stores the passed config object in the configuration registry
// It is equivalent to a restful PUT and is idempotent
func (api *API) AddConfig(request *restful.Request, response *restful.Response) {

	// ToDo: check url name matches body name

	params := request.PathParameters()
	k, err := setup(params)
	if err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	config := &Config{}
	if err = request.ReadEntity(config); err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	if err = config.ParseSpec(); err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	glog.V(2).Infof("Adding config to Istio registry: key %+v, config %+v", k, config)
	// TODO: incorrect use with new registry
	if _, err = api.registry.Post(config.ParsedSpec); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		switch err.(type) {
		case *model.ItemAlreadyExistsError:
			api.writeError(http.StatusConflict, err.Error(), response)
		default:
			api.writeError(http.StatusInternalServerError, err.Error(), response)
		}
		return
	}
	glog.V(2).Infof("Added config %+v", config)
	if err = response.WriteHeaderAndEntity(http.StatusCreated, config); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

// UpdateConfig updates the passed config object in the configuration registry
func (api *API) UpdateConfig(request *restful.Request, response *restful.Response) {

	// ToDo: check url name matches body name

	params := request.PathParameters()
	k, err := setup(params)
	if err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	config := &Config{}
	if err = request.ReadEntity(config); err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	if err = config.ParseSpec(); err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	glog.V(2).Infof("Updating config in Istio registry: key %+v, config %+v", k, config)

	// TODO: incorrect use with new registry
	if _, err = api.registry.Put(config.ParsedSpec, ""); err != nil {
		switch err.(type) {
		case *model.ItemNotFoundError:
			api.writeError(http.StatusNotFound, err.Error(), response)
		default:
			api.writeError(http.StatusInternalServerError, err.Error(), response)
		}
		return
	}
	glog.V(2).Infof("Updated config to %+v", config)
	if err = response.WriteHeaderAndEntity(http.StatusOK, config); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

// DeleteConfig deletes the passed config object in the configuration registry
func (api *API) DeleteConfig(request *restful.Request, response *restful.Response) {

	params := request.PathParameters()
	k, err := setup(params)
	if err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	glog.V(2).Infof("Deleting config from Istio registry: %+v", k)
	// TODO: incorrect use with new registry
	if err = api.registry.Delete(k.Kind, k.Name); err != nil {
		switch err.(type) {
		case *model.ItemNotFoundError:
			api.writeError(http.StatusNotFound, err.Error(), response)
		default:
			api.writeError(http.StatusInternalServerError, err.Error(), response)
		}
		return
	}
	response.WriteHeader(http.StatusOK)
}

// ListConfigs lists the configuration objects in the the configuration registry
// If kind and namespace are passed then it retrieves all rules of a kind in a namespace
// If kind is passed and namespace is an empty string it retrieves all rules of a kind across all namespaces
func (api *API) ListConfigs(request *restful.Request, response *restful.Response) {

	params := request.PathParameters()
	namespace, kind := params["namespace"], params["kind"]

	if _, ok := model.IstioConfigTypes.GetByType(kind); !ok {
		api.writeError(http.StatusBadRequest,
			fmt.Sprintf("unknown configuration type %s; use one of %v", kind, model.IstioConfigTypes.Types()), response)
		return
	}
	glog.V(2).Infof("Getting configs of kind %s in namespace %s", kind, namespace)
	result, err := api.registry.List(kind)
	if err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
		return
	}

	// Parse back to config
	out := []Config{}
	var schema model.ProtoSchema
	for _, v := range result {
		retrieved, errLocal := schema.ToJSON(v.Content)
		if errLocal != nil {
			api.writeError(http.StatusInternalServerError, errLocal.Error(), response)
			return
		}
		var retJSON interface{}
		if errLocal = json.Unmarshal([]byte(retrieved), &retJSON); errLocal != nil {
			api.writeError(http.StatusInternalServerError, errLocal.Error(), response)
			return
		}
		config := Config{
			Name: v.Key,
			Type: v.Type,
			Spec: retJSON,
		}
		glog.V(2).Infof("Retrieved config %+v", config)
		out = append(out, config)
	}
	if err = response.WriteHeaderAndEntity(http.StatusOK, out); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

// Version returns the version information of apiserver
func (api *API) Version(request *restful.Request, response *restful.Response) {
	glog.V(2).Infof("Returning version information")
	if err := response.WriteHeaderAndEntity(http.StatusOK, version.Info); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

func (api *API) writeError(status int, msg string, response *restful.Response) {
	glog.Warning(msg)
	response.AddHeader("Content-Type", "text/plain")
	if err := response.WriteErrorString(status, msg); err != nil {
		glog.Warning(err)
	}
}

type key struct {
	Kind      string
	Name      string
	Namespace string
}

func setup(params map[string]string) (key, error) {
	name, namespace, kind := params["name"], params["namespace"], params["kind"]
	if _, ok := model.IstioConfigTypes.GetByType(kind); !ok {
		return key{}, fmt.Errorf("unknown configuration type %s; use one of %v", kind, model.IstioConfigTypes.Types())
	}

	return key{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
	}, nil
}
