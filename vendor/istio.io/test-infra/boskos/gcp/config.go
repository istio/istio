// Copyright 2018 Istio Authors
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

package gcp

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/container/v1"
	yaml "gopkg.in/yaml.v2"
	"k8s.io/test-infra/boskos/common"
	"k8s.io/test-infra/boskos/mason"
)

var (
	seededRand = rand.New(rand.NewSource(time.Now().UnixNano()))
	// Setting client
	gcpClient     *Client
	gcpClientLock sync.RWMutex
)

const (
	// ResourceConfigType defines the GCP config type
	ResourceConfigType      = "GCPResourceConfig"
	defaultOperationTimeout = 15 * time.Minute
	charset                 = "abcdefghijklmnopqrstuvwxyz1234567890"
	maxChannels             = 100
)

// SetClient sets the gcpClient used to construct a ResourceConfig
func SetClient(c *Client) {
	gcpClientLock.Lock()
	defer gcpClientLock.Unlock()
	gcpClient = c
}

// NewClient creates a new client
func NewClient(serviceAccount string) (*Client, error) {
	var (
		oauthClient *http.Client
		err         error
	)
	if serviceAccount != "" {
		var data []byte
		data, err = ioutil.ReadFile(serviceAccount)
		if err != nil {
			return nil, err
		}
		var conf *jwt.Config
		conf, err = google.JWTConfigFromJSON(data, compute.CloudPlatformScope)
		if err != nil {
			return nil, err
		}
		oauthClient = conf.Client(context.Background())
	} else {
		oauthClient, err = google.DefaultClient(context.Background(), compute.CloudPlatformScope)
		if err != nil {
			return nil, err
		}
	}
	gkeService, err := container.New(oauthClient)
	if err != nil {
		return nil, err
	}
	gceService, err := compute.New(oauthClient)
	if err != nil {
		return nil, err
	}
	return &Client{
		gke:              &containerEngine{gkeService},
		gce:              &computeEngine{gceService},
		operationTimeout: defaultOperationTimeout,
	}, nil
}

type projectConfig struct {
	Clusters []clusterConfig        `json:"clusters,omitempty"`
	Vms      []virtualMachineConfig `json:"vms,omitempty"`
}

// resourceConfigs is resource map of type of resource to list of project config
type resourceConfigs map[string][]projectConfig

// InstanceInfo stores information about a cluster or a vm instance
type InstanceInfo struct {
	Name string `json:"name"`
	Zone string `json:"zone"`
}

// ProjectInfo stores cluster and vm information for a given GCP project
type ProjectInfo struct {
	Clusters []InstanceInfo `json:"clusters,omitempty"`
	VMs      []InstanceInfo `json:"vms,omitempty"`
}

// ResourceInfo holds information about the resource created, such that it can used
type ResourceInfo map[string]ProjectInfo

type vmCreator interface {
	create(context.Context, string, virtualMachineConfig) (*InstanceInfo, error)
	listZones(project string) ([]string, error)
}

type clusterCreator interface {
	create(context.Context, string, clusterConfig) (*InstanceInfo, error)
}

// Client abstracts operation with GCP
type Client struct {
	gke              clusterCreator
	gce              vmCreator
	operationTimeout time.Duration
}

// used for communication between go routine
type com struct {
	p   string
	ci  *InstanceInfo
	vmi *InstanceInfo
}

type stringRing struct {
	index  int
	values []string
}

func (r *stringRing) next() string {
	value := r.values[r.index]
	r.index = (r.index + 1) % len(r.values)
	return value
}

func newStringRing(zones []string) *stringRing {
	return &stringRing{values: zones}
}

// Construct implements Masonable interface
func (rc resourceConfigs) Construct(ctx context.Context, res common.Resource, types common.TypeToResources) (*common.UserData, error) {
	var err error

	if gcpClient == nil {
		err = fmt.Errorf("client not set")
		logrus.WithError(err).Error("client not set; please call SetClient")
		return nil, err
	}

	communication := make(chan com, maxChannels)

	// Copy
	typesCopy := types

	popProject := func(rType string) *common.Resource {
		if len(typesCopy[rType]) == 0 {
			return nil
		}
		r := typesCopy[rType][len(typesCopy[rType])-1]
		typesCopy[rType] = typesCopy[rType][:len(typesCopy[rType])-1]
		return &r
	}

	ctx, cancel := context.WithTimeout(ctx, gcpClient.operationTimeout)
	defer cancel()
	errGroup, derivedCtx := errgroup.WithContext(ctx)

	// Here we know that resources are of project type
	for rType, pcs := range rc {
		for _, pc := range pcs {
			project := popProject(rType)

			if project == nil {
				err = fmt.Errorf("running out of project while creating resources")
				logrus.WithError(err).Errorf("unable to create resources")
				return nil, err
			}
			zones, err := gcpClient.gce.listZones(project.Name)
			if err != nil {
				return nil, err
			}
			zoneRing := newStringRing(zones)

			for i := range pc.Clusters {
				cl := pc.Clusters[i]
				if cl.Zone == "" {
					cl.Zone = zoneRing.next()
				}
				errGroup.Go(func() error {
					clusterInfo, err := gcpClient.gke.create(derivedCtx, project.Name, cl)
					if err != nil {
						logrus.WithError(err).Errorf("unable to create cluster on project %s", project.Name)
						return err
					}
					communication <- com{p: project.Name, ci: clusterInfo}
					return nil
				})
			}
			for j := range pc.Vms {
				vm := pc.Vms[j]
				if vm.Zone == "" {
					vm.Zone = zoneRing.next()
				}
				errGroup.Go(func() error {
					vmInfo, err := gcpClient.gce.create(derivedCtx, project.Name, vm)
					if err != nil {
						logrus.WithError(err).Errorf("unable to create vm on project %s", project.Name)
						return err
					}
					communication <- com{p: project.Name, vmi: vmInfo}
					return nil
				})
			}
		}

	}
	if err := errGroup.Wait(); err != nil {
		logrus.WithError(err).Errorf("failed to construct resources for %s", res.Name)
		return nil, err
	}
	close(communication)

	info := ResourceInfo{}

	for c := range communication {
		pi, exists := info[c.p]
		if !exists {
			pi = ProjectInfo{}
		}
		if c.ci != nil {
			pi.Clusters = append(pi.Clusters, *c.ci)
		} else {
			pi.VMs = append(pi.VMs, *c.vmi)
		}
		info[c.p] = pi
	}

	userData := common.UserData{}
	if err := userData.Set(ResourceConfigType, &info); err != nil {
		logrus.WithError(err).Errorf("unable to set %s user data", ResourceConfigType)
		return nil, err
	}
	return &userData, nil
}

// ConfigConverter implements mason.ConfigConverter
func ConfigConverter(in string) (mason.Masonable, error) {
	var config resourceConfigs
	if err := yaml.Unmarshal([]byte(in), &config); err != nil {
		logrus.WithError(err).Errorf("unable to parse %s", in)
		return nil, err
	}
	return &config, nil
}

func randomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func generateName(prefix string) string {
	date := time.Now().Format("010206")
	randString := randomString(10)
	return fmt.Sprintf("%s-%s-%s", prefix, date, randString)
}

// Install kubeconfig for a given resource. It will create only one file with all contexts.
func (r ResourceInfo) Install(kubeconfig string) error {
	for project, p := range r {
		for _, c := range p.Clusters {
			if err := SetKubeConfig(project, c.Zone, c.Name, kubeconfig); err != nil {
				logrus.WithError(err).Errorf("failed to set kubeconfig")
				return err
			}
		}
	}
	return nil
}
