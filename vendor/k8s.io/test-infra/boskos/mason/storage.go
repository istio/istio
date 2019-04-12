/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mason

import (
	"reflect"
	"sync"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"k8s.io/test-infra/boskos/common"
	"k8s.io/test-infra/boskos/storage"
)

// Storage stores configuration information
type Storage struct {
	configs     storage.PersistenceLayer
	configsLock sync.RWMutex
}

func newStorage(c storage.PersistenceLayer) *Storage {
	return &Storage{
		configs: c,
	}
}

// AddConfig adds a new config
func (s *Storage) AddConfig(conf common.ResourcesConfig) error {
	return s.configs.Add(conf)
}

// DeleteConfig deletes an existing config if it exists or fail otherwise
func (s *Storage) DeleteConfig(name string) error {
	return s.configs.Delete(name)
}

// UpdateConfig updates a given if it exists or fail otherwise
func (s *Storage) UpdateConfig(conf common.ResourcesConfig) error {
	return s.configs.Update(conf)
}

// GetConfig returns an existing if it exists errors out otherwise
func (s *Storage) GetConfig(name string) (common.ResourcesConfig, error) {
	i, err := s.configs.Get(name)
	if err != nil {
		return common.ResourcesConfig{}, err
	}
	var conf common.ResourcesConfig
	conf, err = common.ItemToResourcesConfig(i)
	if err != nil {
		return common.ResourcesConfig{}, err
	}
	return conf, nil
}

// GetConfigs returns all configs
func (s *Storage) GetConfigs() ([]common.ResourcesConfig, error) {
	var configs []common.ResourcesConfig
	items, err := s.configs.List()
	if err != nil {
		return configs, err
	}
	for _, i := range items {
		var conf common.ResourcesConfig
		conf, err = common.ItemToResourcesConfig(i)
		if err != nil {
			return nil, err
		}
		configs = append(configs, conf)
	}
	return configs, nil
}

// SyncConfigs syncs new configs
func (s *Storage) SyncConfigs(newConfigs []common.ResourcesConfig) error {
	s.configsLock.Lock()
	defer s.configsLock.Unlock()

	currentConfigs, err := s.GetConfigs()
	if err != nil {
		logrus.WithError(err).Error("cannot find configs")
		return err
	}

	currentSet := mapset.NewSet()
	newSet := mapset.NewSet()
	toUpdate := mapset.NewSet()

	configs := map[string]common.ResourcesConfig{}

	for _, c := range currentConfigs {
		currentSet.Add(c.Name)
		configs[c.Name] = c
	}

	for _, c := range newConfigs {
		newSet.Add(c.Name)
		if old, exists := configs[c.Name]; exists {
			if !reflect.DeepEqual(old, c) {
				toUpdate.Add(c.Name)
				configs[c.Name] = c
			}
		} else {
			configs[c.Name] = c
		}
	}

	var finalError error

	toDelete := currentSet.Difference(newSet)
	toAdd := newSet.Difference(currentSet)

	for _, n := range toDelete.ToSlice() {
		logrus.Infof("Deleting config %s", n.(string))
		if err := s.DeleteConfig(n.(string)); err != nil {
			logrus.WithError(err).Errorf("failed to delete config %s", n)
			finalError = multierror.Append(finalError, err)
		}
	}

	for _, n := range toAdd.ToSlice() {
		rc := configs[n.(string)]
		logrus.Infof("Adding config %s", n.(string))
		if err := s.AddConfig(rc); err != nil {
			logrus.WithError(err).Errorf("failed to create resources %s", n)
			finalError = multierror.Append(finalError, err)
		}
	}

	for _, n := range toUpdate.ToSlice() {
		rc := configs[n.(string)]
		logrus.Infof("Updating config %s", n.(string))
		if err := s.UpdateConfig(rc); err != nil {
			logrus.WithError(err).Errorf("failed to update resources %s", n)
			finalError = multierror.Append(finalError, err)
		}
	}

	return finalError
}
