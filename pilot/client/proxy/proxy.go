package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/golang/glog"

	"istio.io/manager/apiserver"
	"istio.io/manager/model"
)

// RESTRequester is yet another client wrapper for making REST
// calls. Ideally rest.Interface from "k8s.io/client-go/rest" would be
// used, but that returns not-interface types making it more difficult
// to mock for unit-test, e.g. rest.Request.
type RESTRequester interface {
	Request(method, path string, inBody []byte) (int, []byte, error)
}

// BasicHTTPRequester is a platform neutral requester.
type BasicHTTPRequester struct {
	BaseURL string
	Client  *http.Client
	Version string
}

func toCurl(request *http.Request, body string) string {
	var headers string
	for key, values := range request.Header {
		for _, value := range values {
			headers += fmt.Sprintf(` -H %q`, fmt.Sprintf("%s: %s", key, value))
		}
	}
	var bodyOption string
	if body != "" {
		bodyOption = fmt.Sprintf("--data '%s'", strings.Replace(body, "'", "\\'", -1))
	}
	return fmt.Sprintf("curl -X %v %v %q %s", request.Method, headers, request.URL, bodyOption)
}

// Request sends basic HTTP requests. It does not handle authentication.
func (f *BasicHTTPRequester) Request(method, path string, inBody []byte) (int, []byte, error) {
	host := f.BaseURL
	if !strings.HasPrefix(host, "http://") {
		host = "http://" + host
	}
	absPath := fmt.Sprintf("%s/%s", host, path)
	request, err := http.NewRequest(method, absPath, bytes.NewBuffer(inBody))
	if err != nil {
		return 0, nil, err
	}
	if request.Method == "POST" || request.Method == "PUT" {
		request.Header.Set("Content-Type", "application/json")
	}

	// Log after the call to m.do() so that the full hostname is present
	defer glog.V(2).Infof("%s", toCurl(request, string(inBody)))

	response, err := f.Client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = response.Body.Close() }() // #nosec
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, body, nil
}

// ManagerClient is a client wrapper that contains the base URL and API version
type ManagerClient struct {
	rr RESTRequester
}

// Client defines the interface for the proxy specific functionality of the manager client
type Client interface {
	GetConfig(model.Key) (*apiserver.Config, error)
	AddConfig(model.Key, apiserver.Config) error
	UpdateConfig(model.Key, apiserver.Config) error
	DeleteConfig(model.Key) error
	ListConfig(string, string) ([]apiserver.Config, error)
}

// NewManagerClient creates a new ManagerClient instance. It trims the apiVersion of leading and trailing slashes
// and the base path of trailing slashes to ensure consistency
func NewManagerClient(rr RESTRequester) *ManagerClient {
	return &ManagerClient{rr: rr}
}

func (m *ManagerClient) doConfigCRUD(key model.Key, method string, inBody []byte) ([]byte, error) {
	uriSuffix := fmt.Sprintf("config/%v/%v/%v", key.Kind, key.Namespace, key.Name)
	status, body, err := m.rr.Request(method, uriSuffix, inBody)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		if len(body) == 0 {
			return nil, fmt.Errorf("received non-success status code %v", status)
		}
		return nil, fmt.Errorf("received non-success status code %v with message %v", status, string(body))
	}
	return body, nil
}

// GetConfig retrieves the configuration resource for the passed key
func (m *ManagerClient) GetConfig(key model.Key) (*apiserver.Config, error) {
	body, err := m.doConfigCRUD(key, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	config := &apiserver.Config{}
	if err := json.Unmarshal(body, config); err != nil {
		return nil, err
	}
	return config, nil
}

// AddConfig creates a configuration resources for the passed key using the passed configuration
// It is idempotent
func (m *ManagerClient) AddConfig(key model.Key, config apiserver.Config) error {
	bodyIn, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if _, err = m.doConfigCRUD(key, http.MethodPost, bodyIn); err != nil {
		return err
	}
	return nil
}

// UpdateConfig updates the configuration resource for the passed key using the passed configuration
// It is idempotent
func (m *ManagerClient) UpdateConfig(key model.Key, config apiserver.Config) error {
	bodyIn, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if _, err = m.doConfigCRUD(key, http.MethodPut, bodyIn); err != nil {
		return err
	}
	return nil
}

// DeleteConfig deletes the configuration resource for the passed key
func (m *ManagerClient) DeleteConfig(key model.Key) error {
	_, err := m.doConfigCRUD(key, http.MethodDelete, nil)
	return err
}

// ListConfig retrieves all configuration resources of the passed kind in the given namespace
// If namespace is an empty string it retrieves all configs of the passed kind across all namespaces
func (m *ManagerClient) ListConfig(kind, namespace string) ([]apiserver.Config, error) {
	var reqURL string
	if namespace != "" {
		reqURL = fmt.Sprintf("config/%v/%v", kind, namespace)
	} else {
		reqURL = fmt.Sprintf("config/%v", kind)
	}
	_, body, err := m.rr.Request(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	var config []apiserver.Config
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, err
	}
	glog.V(2).Infof("Response Body:  %v", string(body))
	return config, nil
}
