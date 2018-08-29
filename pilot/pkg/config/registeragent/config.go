package config

import (
	log "github.com/cihub/seelog"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path"
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
