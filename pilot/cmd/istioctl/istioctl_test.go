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

package main

import (
	"errors"
	"fmt"
	"testing"

	"strings"

	"istio.io/manager/apiserver"
	"istio.io/manager/model"
)

type StubClient struct {
	AddConfigCalled bool
	KeyConfigMap    map[model.Key]apiserver.Config
	WantKeys        map[model.Key]struct{}
	Error           error
}

func (st *StubClient) GetConfig(model.Key) (*apiserver.Config, error) {
	if st.Error != nil {
		return nil, st.Error
	}
	config, ok := st.KeyConfigMap[key]
	if !ok {
		return nil, fmt.Errorf("received unexpected key: %v", key)
	}
	if err := config.ParseSpec(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (st *StubClient) AddConfig(key model.Key, config apiserver.Config) error {
	st.AddConfigCalled = true
	if st.Error != nil {
		return st.Error
	}
	return st.verifyKeyConfig(key, config)
}

func (st *StubClient) UpdateConfig(key model.Key, config apiserver.Config) error {
	if st.Error != nil {
		return st.Error
	}
	return st.verifyKeyConfig(key, config)
}

func (st *StubClient) DeleteConfig(key model.Key) error {
	if st.Error != nil {
		return st.Error
	}
	if _, ok := st.WantKeys[key]; !ok {
		return fmt.Errorf("received unexpected key: %v", key)
	}
	return nil

}

func (st *StubClient) ListConfig(string, string) ([]apiserver.Config, error) {
	if st.Error != nil {
		return nil, st.Error
	}
	var res []apiserver.Config
	for _, config := range st.KeyConfigMap {
		if err := config.ParseSpec(); err != nil {
			return nil, err
		}
		res = append(res, config)
	}
	return res, nil
}

func (st *StubClient) setupTwoRouteRuleMap() {
	st.KeyConfigMap = make(map[model.Key]apiserver.Config)
	key1 := model.Key{
		Name:      "test-v1",
		Namespace: namespace,
		Kind:      "route-rule",
	}
	key2 := model.Key{
		Name:      "test-v2",
		Namespace: namespace,
		Kind:      "route-rule",
	}
	st.KeyConfigMap[key1] = apiserver.Config{
		Name: "test-v1",
		Type: "route-rule",
	}
	st.KeyConfigMap[key2] = apiserver.Config{
		Name: "test-v2",
		Type: "route-rule",
	}
}

func (st *StubClient) setupDeleteKeys() {
	st.WantKeys = make(map[model.Key]struct{})
	st.WantKeys[model.Key{Name: "test-v1", Namespace: "default", Kind: "route-rule"}] = struct{}{}
	st.WantKeys[model.Key{Name: "test-v2", Namespace: "default", Kind: "route-rule"}] = struct{}{}
}

func (st *StubClient) verifyKeyConfig(key model.Key, config apiserver.Config) error {
	wantConfig, ok := st.KeyConfigMap[key]
	if !ok {
		return fmt.Errorf("received unexpected key/config pair\n key: %+v\nconfig: %+v", key, config)
	}
	// ToDo: test spec as well
	if strings.Compare(wantConfig.Name, config.Name) != 0 {
		return fmt.Errorf("received unexpected config name: %s, wanted: %s", config.Name, wantConfig.Name)
	}
	if strings.Compare(wantConfig.Type, config.Type) != 0 {
		return fmt.Errorf("received unexpected config type: %s, wanted: %s", config.Type, wantConfig.Type)
	}
	return nil
}

func TestClientSideValidation(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{
			name: "TestCreateInvalidFile",
			file: "does-not-exist.yaml",
		},
		{
			name: "TestInvalidType",
			file: "testdata/invalid-type.yaml",
		},
		{
			name: "TestInvalidRouteRule",
			file: "testdata/invalid-route-rule.yaml",
		},
		{
			name: "TestInvalidDestinationPolicy",
			file: "testdata/invalid-destination-policy2.yaml",
		},
	}

	for _, c := range cases {
		stubClient := &StubClient{}
		apiClient = stubClient
		file = c.file
		if err := postCmd.RunE(postCmd, []string{}); err == nil {
			t.Fatalf("%s failed: %v", c.name, err)
		}
		if stubClient.AddConfigCalled {
			t.Fatalf("%s failed: AddConfig was called but it should have errored prior to calling", c.name)
		}
	}
}

func TestCreateUpdateDeleteGet(t *testing.T) {

	cases := []struct {
		name              string
		command           string
		file              string
		configKeyMapReq   bool
		deleteKeySliceReq bool
		wantError         bool
		arg               []string
		outFormat         string
	}{
		{
			name:            "TestCreateSuccess",
			command:         "post",
			file:            "testdata/two-route-rules.yaml",
			configKeyMapReq: true,
		},
		{
			name:      "TestCreateErrorsPassedBack",
			command:   "post",
			file:      "testdata/two-route-rules.yaml",
			wantError: true,
		},
		{
			name:      "TestCreateErrorsWithArg",
			command:   "post",
			file:      "testdata/two-route-rules.yaml",
			wantError: true,
			arg:       []string{"arg-im-a-pirate"},
		},
		{
			name:      "TestCreateNoFile",
			command:   "post",
			wantError: true,
		},
		{
			name:            "TestUpdateSuccess",
			command:         "put",
			file:            "testdata/two-route-rules.yaml",
			configKeyMapReq: true,
		},
		{
			name:      "TestUpdateErrorsPassedBack",
			command:   "put",
			file:      "testdata/two-route-rules.yaml",
			wantError: true,
		},
		{
			name:      "TestUpdateErrorsWithArg",
			command:   "put",
			file:      "testdata/two-route-rules.yaml",
			wantError: true,
			arg:       []string{"arg-im-a-pirate"},
		},
		{
			name:      "TestUpdateNoFile",
			command:   "put",
			wantError: true,
		},
		{
			name:              "TestDeleteSuccessWithFile",
			command:           "delete",
			deleteKeySliceReq: true,
			file:              "testdata/two-route-rules.yaml",
		},
		{
			name:      "TestDeleteArgsErrorWithFile",
			command:   "delete",
			file:      "testdata/two-route-rules.yaml",
			arg:       []string{"arg-im-a-pirate"},
			wantError: true,
		},
		{
			name:      "TestDeleteNoArgsErrorWithoutFile",
			command:   "delete",
			wantError: true,
		},
		{
			name:      "TestDeleteErrorsPassedBackWithFile",
			command:   "delete",
			file:      "testdata/two-route-rules.yaml",
			wantError: true,
		},
		{
			name:              "TestDeleteSuccessWithoutFile",
			command:           "delete",
			arg:               []string{"route-rule", "test-v1"},
			deleteKeySliceReq: true,
		},
		{
			name:      "TestDeleteErrorsPassedBackWithoutFile",
			command:   "delete",
			arg:       []string{"route-rule", "test-v1"},
			wantError: true,
		},
		{
			name:      "TestGetNoArgs",
			command:   "get",
			wantError: true,
		},
		{
			name:      "TestListPassesBackErrors",
			command:   "get",
			arg:       []string{"route-rule"},
			wantError: true,
			outFormat: "short",
		},
		{
			name:            "TestGetRouteRule",
			command:         "get",
			arg:             []string{"route-rule"},
			configKeyMapReq: true,
			outFormat:       "short",
		},
		{
			name:            "TestGetRouteRules",
			command:         "get",
			arg:             []string{"route-rules"},
			configKeyMapReq: true,
			outFormat:       "short",
		},
		{
			name:            "TestGetDestPolicy",
			command:         "get",
			arg:             []string{"destination-policy"},
			configKeyMapReq: true,
			outFormat:       "short",
		},
		{
			name:            "TestGetDestPolicies",
			command:         "get",
			arg:             []string{"destination-policies"},
			configKeyMapReq: true,
			outFormat:       "short",
		},
		{
			name:            "TestGetYAML",
			command:         "get",
			arg:             []string{"route-rule"},
			configKeyMapReq: true,
			outFormat:       "yaml",
		},
		{
			name:            "TestGetNotYAMLOrShort",
			command:         "get",
			arg:             []string{"route-rule"},
			configKeyMapReq: true,
			outFormat:       "not-an-output-format",
			wantError:       true,
		},
		{
			name:            "TestGetRouteRuleByName",
			command:         "get",
			arg:             []string{"route-rule", "test-v1"},
			configKeyMapReq: true,
			outFormat:       "short",
		},
		{
			name:      "TestGetRouteRuleByNamePassesBackErrors",
			command:   "get",
			arg:       []string{"route-rule", "test-v1"},
			wantError: true,
			outFormat: "short",
		},
	}

	for _, c := range cases {
		stubClient := &StubClient{}
		apiClient = stubClient
		if c.configKeyMapReq {
			stubClient.setupTwoRouteRuleMap()
		}
		if c.deleteKeySliceReq {
			stubClient.setupDeleteKeys()
		}
		if c.wantError {
			stubClient.Error = errors.New("an error")
		}
		outputFormat = c.outFormat
		file = c.file

		var err error
		switch c.command {
		case "post":
			err = postCmd.RunE(postCmd, c.arg)
		case "put":
			err = putCmd.RunE(putCmd, c.arg)
		case "delete":
			err = deleteCmd.RunE(deleteCmd, c.arg)
		case "get":
			err = getCmd.RunE(getCmd, c.arg)
		}
		if err != nil && !c.wantError {
			t.Fatalf("%v: %v", c.name, err)
		} else if err == nil && c.wantError {
			t.Fatalf("%v: expected an error", c.name)
		}
	}
}
