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
	"io/ioutil"
	"os"
	"path"
	"istio.io/istio/pkg/log"
	"gopkg.in/yaml.v2"
)

type Config struct {
	// cache update interval time
	cache_interval_time string `yaml:"cache_interval_time"`
}

func NewConfig() *Config {
	baseDir, err := os.Getwd()
	if err != nil {
		log.Errorf("get pwd error : %v", err)
		panic(err)
	}
	configFile := path.Join(baseDir, "config", "agent.yaml")

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Errorf("read config file(%v) error : %v", configFile, err)
		panic(err)
	}

	config := Config{}
	err = yaml.Unmarshal([]byte(data), &config)
	if err != nil {
		log.Errorf("unmarshal config file(%v) error : %v", configFile, err)
		panic(err)
	}

	return &config
}
