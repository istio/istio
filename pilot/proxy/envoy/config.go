// Copyright 2016 Google Inc.
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

package envoy

import (
	"encoding/json"
	"io"
	"os"
	"sort"

	"istio.io/manager/model"

	"fmt"
	"io/ioutil"
	"math/rand"
	"path/filepath"
)

const EnvoyConfigPath = "/etc/envoy/envoy.json"

func writeConfigFile(conf Config) error {
	file, err := os.Create(EnvoyConfigPath)
	if err != nil {
		return err
	}

	if err := writeConfig(file, conf); err != nil {
		file.Close()
		return err
	}

	return file.Close()
}

func writeConfig(w io.Writer, conf Config) error {
	out, err := json.MarshalIndent(&conf, "", "  ")
	if err != nil {
		return err
	}

	_, err = w.Write(out)
	if err != nil {
		return err
	}

	return err
}

func generateConfig(services []model.Service, serviceName string, tags []string) (Config, error) {
	clusters := buildClusters()
	routes := buildRoutes()
	filters := buildFaults(serviceName, tags)

	if err := buildFS(); err != nil {
		return Config{}, err
	}

	return Config{
		RootRuntime: RootRuntime{
			SymlinkRoot:  RuntimePath,
			Subdirectory: "traffic_shift",
		},
		Listeners: []Listener{
			{
				Port: 6379,
				Filters: []NetworkFilter{
					{
						Type: "read",
						Name: "http_connection_manager",
						Config: NetworkFilterConfig{
							CodecType:  "auto",
							StatPrefix: "ingress_http",
							RouteConfig: RouteConfig{
								VirtualHosts: []VirtualHost{
									{
										Name:    "backend",
										Domains: []string{"*"},
										Routes:  routes,
									},
								},
							},
							Filters: filters,
							AccessLog: []AccessLog{
								{
									Path: "/var/log/envoy_access.log",
								},
							},
						},
					},
				},
			},
		},
		Admin: Admin{
			AccessLogPath: "/var/log/envoy_admin.log",
			Port:          8001,
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: SDS{
				Cluster: Cluster{
					Name:             "sds",
					Type:             "strict_dns",
					ConnectTimeoutMs: 1000,
					LbType:           "round_robin",
					Hosts: []Host{
						{
							URL: "tcp://127.0.0.1:6500",
						},
					},
				},
				RefreshDelayMs: 1000,
			},
		},
	}, nil
}

func buildClusters() []Cluster {
	clusterMap := make(map[string]struct{})
	clusters := make([]Cluster, 0, len(clusterMap))
	for clusterName := range clusterMap {
		cluster := Cluster{
			Name:             clusterName,
			ServiceName:      clusterName,
			Type:             "sds",
			LbType:           "round_robin",
			ConnectTimeoutMs: 1000,
		}

		clusters = append(clusters, cluster)
	}

	sort.Sort(ClustersByName(clusters))

	return clusters
}

func buildRoutes() []Route {
	routes := []Route{}
	return routes
}

const (
	ConfigPath          = "/etc/envoy"
	RuntimePath         = "/etc/envoy/runtime/routing"
	RuntimeVersionsPath = "/etc/envoy/routing_versions"

	ConfigDirPerm  = 0775
	ConfigFilePerm = 0664
)

// FIXME: doesn't check for name conflicts
// TODO: could be improved by using the full possible set of filenames.
func randFilename(prefix string) string {
	data := make([]byte, 16)
	for i := range data {
		data[i] = '0' + byte(rand.Intn(10))
	}

	return fmt.Sprintf("%s%s", prefix, data)
}

func buildFS() error {
	type weightSpec struct {
		Service string
		Cluster string
		Weight  int
	}

	var weights []weightSpec

	if err := os.MkdirAll(filepath.Dir(RuntimePath), ConfigDirPerm); err != nil { // FIXME: hack
		return err
	}

	if err := os.MkdirAll(RuntimeVersionsPath, ConfigDirPerm); err != nil {
		return err
	}

	dirName, err := ioutil.TempDir(RuntimeVersionsPath, "")
	if err != nil {
		return err
	}

	success := false
	defer func() {
		if !success {
			os.RemoveAll(dirName)
		}
	}()

	for _, weight := range weights {
		if err := os.MkdirAll(filepath.Join(dirName, "/traffic_shift/", weight.Service), ConfigDirPerm); err != nil {
			return err
		} // FIXME: filemode?

		filename := filepath.Join(dirName, "/traffic_shift/", weight.Service, weight.Cluster)
		data := []byte(fmt.Sprintf("%v", weight.Weight))
		if err := ioutil.WriteFile(filename, data, ConfigFilePerm); err != nil {
			return err
		}
	}

	oldRuntime, err := os.Readlink(RuntimePath)
	if err != nil && !os.IsNotExist(err) { // Ignore error from symlink not existing.
		return err
	}

	tmpName := randFilename("./")

	if err := os.Symlink(dirName, tmpName); err != nil {
		return err
	}

	// Atomically replace the runtime symlink
	if err := os.Rename(tmpName, RuntimePath); err != nil {
		return err
	}

	success = true

	// Clean up the old config FS if necessary
	// TODO: make this safer
	if oldRuntime != "" {
		oldRuntimeDir := filepath.Dir(oldRuntime)
		if filepath.Clean(oldRuntimeDir) == filepath.Clean(RuntimeVersionsPath) {
			toDelete := filepath.Join(RuntimeVersionsPath, filepath.Base(oldRuntime))
			if err := os.RemoveAll(toDelete); err != nil {
				return err
			}
		}
	}

	return nil
}

func buildFaults(serviceName string, tags []string) []Filter {
	var filters []Filter

	tagMap := make(map[string]struct{})
	for _, tag := range tags {
		tagMap[tag] = struct{}{}
	}

	filters = append(filters, Filter{
		Type:   "decoder",
		Name:   "router",
		Config: FilterRouterConfig{},
	})

	return filters
}
