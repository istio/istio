package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"istio.io/manager/apiserver"
	"istio.io/manager/model"
)

// ManagerClient is a client wrapper that contains the base URL and API version
type ManagerClient struct {
	base             url.URL
	versionedAPIPath string
	client           *http.Client
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
func NewManagerClient(base url.URL, apiVersion string, client *http.Client) *ManagerClient {
	base.Path = strings.TrimSuffix(base.Path, "/")
	return &ManagerClient{
		base:             base,
		versionedAPIPath: strings.TrimPrefix(strings.TrimSuffix(apiVersion, "/"), "/"),
		client:           client,
	}
}

func (m *ManagerClient) do(request *http.Request) (*http.Response, error) {
	fullURL, err := url.Parse(fmt.Sprintf("%s/%s/%s",
		m.base.String(), m.versionedAPIPath, request.URL.String()))
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL: %v", err)
	}
	request.URL = fullURL
	if request.Method == "POST" || request.Method == "PUT" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := m.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer func() { _ = response.Body.Close() }() // #nosec
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return nil, err
		} else if len(body) == 0 {
			return nil, fmt.Errorf("received non-success status code %v", response.StatusCode)
		}
		return nil, fmt.Errorf("received non-success status code %v with message %v", response.StatusCode, string(body))
	}
	return response, nil
}

// GetConfig retrieves the configuration resource for the passed key
func (m *ManagerClient) GetConfig(key model.Key) (config *apiserver.Config, err error) {
	response, err := m.doConfigCRUD(key, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}

	defer func() { _ = response.Body.Close() }() // #nosec
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	config = &apiserver.Config{}
	if err = json.Unmarshal(body, config); err != nil {
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
func (m *ManagerClient) ListConfig(kind, namespace string) (config []apiserver.Config, err error) {
	var reqURL string
	if namespace != "" {
		reqURL = fmt.Sprintf("config/%v/%v", kind, namespace)
	} else {
		reqURL = fmt.Sprintf("config/%v", kind)
	}
	request, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := m.do(request)
	if err != nil {
		return nil, err
	}

	defer func() { _ = response.Body.Close() }() // #nosec
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	config = []apiserver.Config{}
	if err = json.Unmarshal(body, &config); err != nil {
		return nil, err
	}
	return config, nil
}

func (m *ManagerClient) doConfigCRUD(key model.Key, method string, inBody []byte) (*http.Response, error) {
	reqURL := fmt.Sprintf("config/%v/%v/%v", key.Kind, key.Namespace, key.Name)
	var body io.Reader
	if len(inBody) > 0 {
		body = bytes.NewBuffer(inBody)
	}
	request, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}
	return m.do(request)
}
