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
	"math/rand"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/api/compute/v1"
)

const (
	// ResourceConfigType defines the GCP config type
	persistent   = "PERSISTENT"
	oneToOneNAT  = "ONE_TO_ONE_NAT"
	zoneUSPrefix = "us-"
)

type virtualMachineConfig struct {
	MachineType string   `json:"machinetype,omitempty"`
	SourceImage string   `json:"sourceimage,omitempty"`
	Zone        string   `json:"zone,ompitempty"`
	Tags        []string `json:"tags,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

type computeEngine struct {
	service *compute.Service
}

func (cc *computeEngine) listZones(project string) ([]string, error) {
	zoneList, err := cc.service.Zones.List(project).Do()
	if err != nil {
		return nil, err
	}
	var zones []string
	for _, z := range zoneList.Items {
		if strings.HasPrefix(z.Name, zoneUSPrefix) {
			zones = append(zones, z.Name)
		}
	}
	if len(zones) == 0 {
		return nil, fmt.Errorf("no zone found")
	}
	logrus.Infof("Found zones %v for project %s", zones, project)
	// shuffling data to pick different zones across projects
	rand.Shuffle(len(zones), func(i, j int) {
		zones[i], zones[j] = zones[j], zones[i]
	})
	return zones, nil
}

func (cc *computeEngine) waitForOperation(ctx context.Context, op *compute.Operation, project, zone string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(defaultSleepTime):
			newOp, err := cc.service.ZoneOperations.Get(project, zone, op.Name).Context(ctx).Do()
			if err != nil {
				return err
			}
			switch newOp.Status {
			case operationDone:
				return nil
			case operationAborting:
				return fmt.Errorf(newOp.StatusMessage)
			default:
				logrus.Infof("operation %s status is %s", newOp.Name, newOp.Status)
			}

		}
	}
}

func newComputeInstance(config virtualMachineConfig, project, name string) *compute.Instance {
	// Inconsistency between compute and container APIs
	machineType := fmt.Sprintf("projects/%s/zones/%s/machineTypes/%s", project, config.Zone, config.MachineType)
	zone := fmt.Sprintf("projects/%s/zones/%s", project, config.Zone)
	instance := &compute.Instance{
		Name:         name,
		Zone:         zone,
		MachineType:  machineType,
		CanIpForward: true,
		Disks: []*compute.AttachedDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Type:       persistent,
				InitializeParams: &compute.AttachedDiskInitializeParams{
					DiskName:    name,
					SourceImage: config.SourceImage,
				},
			},
		},
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				AccessConfigs: []*compute.AccessConfig{
					{
						Name: "External NAT",
						Type: oneToOneNAT,
					},
				},
			},
		},
		ServiceAccounts: []*compute.ServiceAccount{
			{
				Email:  "default",
				Scopes: config.Scopes,
			},
		},
	}
	if config.Tags != nil {
		instance.Tags = &compute.Tags{Items: config.Tags}
	}
	return instance
}

func (cc *computeEngine) create(ctx context.Context, project string, config virtualMachineConfig) (*InstanceInfo, error) {
	name := generateName("gce")
	instance := newComputeInstance(config, project, name)
	op, err := cc.service.Instances.Insert(project, config.Zone, instance).Context(ctx).Do()
	if err != nil {
		logrus.WithError(err).Errorf("failed to create %v on project %s", instance, project)
		return nil, err
	}
	logrus.Infof("Instance %s being created via operation %s, waiting for completion", instance.Name, op.Name)
	if err := cc.waitForOperation(ctx, op, project, config.Zone); err != nil {
		logrus.WithError(err).Errorf("operation %s failed", op.Name)
		return nil, err
	}
	logrus.Infof("Instance %s created via operation %s", instance.Name, op.Name)
	return &InstanceInfo{Name: name, Zone: config.Zone}, nil
}
