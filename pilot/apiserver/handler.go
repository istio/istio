package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"istio.io/manager/model"

	restful "github.com/emicklei/go-restful"
	"github.com/golang/glog"
)

// GetConfig retrieves the config object from the configuration registry
func (api *API) GetConfig(request *restful.Request, response *restful.Response) {

	params := request.PathParameters()
	key, err := setup(params)
	if err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	glog.V(2).Infof("Getting config from Istio registry: %+v", key)
	proto, ok := api.registry.Get(key)
	if !ok {
		errLocal := &model.ItemNotFoundError{Key: key}
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
	if err = response.WriteHeaderAndEntity(http.StatusOK, config); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

// AddConfig creates and stores the passed config object in the configuration registry
// It is equivalent to a restful PUT and is idempotent
func (api *API) AddConfig(request *restful.Request, response *restful.Response) {

	// ToDo: check url name matches body name

	params := request.PathParameters()
	key, err := setup(params)
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

	glog.V(2).Infof("Adding config to Istio registry: key %+v, config %+v", key, config)
	if err = api.registry.Post(key, config.ParsedSpec); err != nil {
		response.AddHeader("Content-Type", "text/plain")
		switch err.(type) {
		case *model.ItemAlreadyExistsError:
			api.writeError(http.StatusConflict, err.Error(), response)
		default:
			api.writeError(http.StatusInternalServerError, err.Error(), response)
		}
		return
	}
	if err = response.WriteHeaderAndEntity(http.StatusCreated, config); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

// UpdateConfig updates the passed config object in the configuration registry
func (api *API) UpdateConfig(request *restful.Request, response *restful.Response) {

	// ToDo: check url name matches body name

	params := request.PathParameters()
	key, err := setup(params)
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

	glog.V(2).Infof("Updating config in Istio registry: key %+v, config %+v", key, config)
	if err = api.registry.Put(key, config.ParsedSpec); err != nil {
		switch err.(type) {
		case *model.ItemNotFoundError:
			api.writeError(http.StatusNotFound, err.Error(), response)
		default:
			api.writeError(http.StatusInternalServerError, err.Error(), response)
		}
		return
	}
	if err = response.WriteHeaderAndEntity(http.StatusOK, config); err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
	}
}

// DeleteConfig deletes the passed config object in the configuration registry
func (api *API) DeleteConfig(request *restful.Request, response *restful.Response) {

	params := request.PathParameters()
	key, err := setup(params)
	if err != nil {
		api.writeError(http.StatusBadRequest, err.Error(), response)
		return
	}

	glog.V(2).Infof("Deleting config from Istio registry: %+v", key)
	if err = api.registry.Delete(key); err != nil {
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

	if _, ok := model.IstioConfig[kind]; !ok {
		api.writeError(http.StatusBadRequest,
			fmt.Sprintf("unknown configuration type %s; use one of %v", kind, model.IstioConfig.Kinds()), response)
		return
	}

	glog.V(2).Infof("Getting configs of kind %s in namespace %s", kind, namespace)
	result, err := api.registry.List(kind, namespace)
	if err != nil {
		api.writeError(http.StatusInternalServerError, err.Error(), response)
		return
	}

	// Parse back to config
	out := []Config{}
	var schema model.ProtoSchema
	for k, v := range result {
		retrieved, errLocal := schema.ToJSON(v)
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
			Name: k.Name,
			Type: k.Kind,
			Spec: retJSON,
		}
		out = append(out, config)
	}
	if err = response.WriteHeaderAndEntity(http.StatusOK, out); err != nil {
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

func setup(params map[string]string) (model.Key, error) {
	name, namespace, kind := params["name"], params["namespace"], params["kind"]
	if _, ok := model.IstioConfig[kind]; !ok {
		return model.Key{}, fmt.Errorf("unknown configuration type %s; use one of %v", kind, model.IstioConfig.Kinds())
	}

	return model.Key{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
	}, nil
}
