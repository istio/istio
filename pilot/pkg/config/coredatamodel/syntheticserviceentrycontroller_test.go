// Copyright 2019 Istio Authors
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

package coredatamodel_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schemas"
)

type FakeXdsUpdater struct {
	Events    chan string
	Endpoints chan []*model.IstioEndpoint
	EDSErr    chan error
}

func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		EDSErr:    make(chan error, 100),
		Events:    make(chan string, 100),
		Endpoints: make(chan []*model.IstioEndpoint, 100),
	}
}

func (f *FakeXdsUpdater) ConfigUpdate(req *model.PushRequest) {
	f.Events <- "ConfigUpdate"
}

func (f *FakeXdsUpdater) EDSUpdate(shard, hostname, ns string, entry []*model.IstioEndpoint) error {
	f.Events <- "EDSUpdate"
	f.Endpoints <- entry
	return <-f.EDSErr
}

func (f *FakeXdsUpdater) SvcUpdate(shard, hostname string, ports map[string]uint32, rports map[uint32]string) {
}

func (f *FakeXdsUpdater) WorkloadUpdate(id string, labels map[string]string, annotations map[string]string) {
}

func (f *FakeXdsUpdater) ProxyUpdate(clusterID, ip string) {
}

func TestIncrementalControllerHasSynced(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)
	g.Expect(controller.HasSynced()).To(gomega.BeFalse())

	for i, se := range []*networking.ServiceEntry{syntheticServiceEntry0, syntheticServiceEntry1} {
		message := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, se)

		change := convertToChange([]proto.Message{message},
			[]string{fmt.Sprintf("random-namespace/test-synthetic-se-%d", i)},
			setVersion("1"),
			setCollection(schemas.SyntheticServiceEntry.Collection),
			setTypeURL(schemas.SyntheticServiceEntry.MessageName))

		err := controller.Apply(change)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(controller.HasSynced()).To(gomega.BeTrue())
	}
}

func TestIncrementalControllerConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	descriptor := controller.ConfigDescriptor()
	g.Expect(descriptor.Types()).To(gomega.HaveLen(1))
	g.Expect(descriptor.Types()).To(gomega.ContainElement(schemas.SyntheticServiceEntry.Type))
}

func TestIncrementalControllerListInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	c, err := controller.List("gateway", "some-phony-name-space")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("list unknown type gateway"))
}

func TestIncrementalControllerListCorrectTypeNoData(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	c, err := controller.List(schemas.SyntheticServiceEntry.Type, "some-phony-name-space")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).ToNot(gomega.HaveOccurred())
}

func TestIncrementalControllerListAllNameSpace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	syntheticServiceEntry2 := proto.Clone(syntheticServiceEntry1).(*networking.ServiceEntry)
	syntheticServiceEntry2.Ports = serviceEntry.Ports
	syntheticServiceEntry2.Endpoints = serviceEntry.Endpoints

	messages := convertToResources(g,
		schemas.SyntheticServiceEntry.MessageName,
		[]proto.Message{syntheticServiceEntry0, syntheticServiceEntry1, syntheticServiceEntry2})

	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"default/sse-0", "namespace2/sse-1", "namespace3/sse-2"},
		setVersion("1"),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(3))

	for _, conf := range c {
		g.Expect(conf.Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
		switch conf.Name {
		case "sse-0":
			g.Expect(conf.Spec).To(gomega.Equal(message))
			g.Expect(conf.Namespace).To(gomega.Equal("default"))
		case "sse-1":
			g.Expect(conf.Namespace).To(gomega.Equal("namespace2"))
			g.Expect(conf.Spec).To(gomega.Equal(message2))
		case "sse-2":
			g.Expect(conf.Namespace).To(gomega.Equal("namespace3"))
			g.Expect(conf.Spec).To(gomega.Equal(message3))
		}
	}
}

func TestIncrementalControllerListSpecificNameSpace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	syntheticServiceEntry2 := proto.Clone(syntheticServiceEntry1).(*networking.ServiceEntry)
	syntheticServiceEntry2.Ports = serviceEntry.Ports
	syntheticServiceEntry2.Endpoints = serviceEntry.Endpoints

	messages := convertToResources(g,
		schemas.SyntheticServiceEntry.MessageName,
		[]proto.Message{syntheticServiceEntry0, syntheticServiceEntry1, syntheticServiceEntry2})

	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"default/sse-0", "namespace2/sse-1", "namespace2/sse-2"},
		setVersion("1"),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(schemas.SyntheticServiceEntry.Type, "default")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("sse-0"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
	g.Expect(c[0].Namespace).To(gomega.Equal("default"))
	g.Expect(c[0].Spec).To(gomega.Equal(message))

	c, err = controller.List(schemas.SyntheticServiceEntry.Type, "namespace2")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(2))
	for _, conf := range c {
		g.Expect(conf.Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
		g.Expect(conf.Namespace).To(gomega.Equal("namespace2"))
		switch conf.Name {
		case "sse-1":
			g.Expect(conf.Spec).To(gomega.Equal(message2))
		case "sse-2":
			g.Expect(conf.Spec).To(gomega.Equal(message3))
		}
	}
}

func TestIncrementalControllerApplyInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message := convertToResource(g,
		schemas.Gateway.MessageName,
		gateway)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"some-gateway"},
		setVersion("1"),
		setCollection(schemas.Gateway.Collection),
		setTypeURL(schemas.Gateway.Type))

	err := controller.Apply(change)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring(fmt.Sprintf("apply: type not supported %s", schemas.Gateway.Collection)))
}

func TestIncrementalControllerApplyMetadataNameIncludesNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-synthetic-se"},
		setVersion("1"),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(schemas.SyntheticServiceEntry.Type, "random-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("test-synthetic-se"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message))
}

func TestIncrementalControllerApplyMetadataNameWithoutNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	fx.EDSErr <- nil
	testControllerOptions.XDSUpdater = fx
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message0 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)
	change0 := convertToChange([]proto.Message{message0},
		[]string{"synthetic-se-0"},
		setVersion("1"),
		setIncremental(),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change0)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	message1 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry1)
	change1 := convertToChange([]proto.Message{message1},
		[]string{"synthetic-se-1"},
		setVersion("1"),
		setIncremental(),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err = controller.Apply(change1)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(2))
	for _, se := range c {
		g.Expect(se.Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
		switch se.Name {
		case "synthetic-se-0":
			g.Expect(se.Spec).To(gomega.Equal(message0))
		case "synthetic-se-1":
			g.Expect(se.Spec).To(gomega.Equal(message1))
		}
	}
}

func TestIncrementalControllerApplyChangeNoObjects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)
	change := convertToChange([]proto.Message{message},
		[]string{"synthetic-se-0"},
		setVersion("1"),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("synthetic-se-0"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message))

	change = convertToChange([]proto.Message{},
		[]string{"some-synthetic-se"},
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err = controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	// still expecting the old config
	g.Expect(c[0].Name).To(gomega.Equal("synthetic-se-0"))
	g.Expect(c[0].Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
	g.Expect(c[0].Spec).To(gomega.Equal(message))
}

func TestIncrementalControllerApplyInvalidResource(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	se := proto.Clone(syntheticServiceEntry1).(*networking.ServiceEntry)
	se.Hosts = nil

	message0 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, se)

	change := convertToChange(
		[]proto.Message{message0},
		[]string{"bar-namespace/foo"},
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
}

func TestIncrementalControllerApplyInvalidResource_BadTimestamp(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message0 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)
	change := convertToChange(
		[]proto.Message{message0},
		[]string{"bar-namespace/foo"},
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))
	change.Objects[0].Metadata.CreateTime = &types.Timestamp{
		Seconds: -1,
		Nanos:   -1,
	}

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
}

func TestApplyNonIncrementalChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-synthetic-se"},
		setVersion("1"),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	change = convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-synthetic-se1"},
		setVersion("1"),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se1"))

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))
}

func TestApplyNonIncrementalAnnotations(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/17814")
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	fx.EDSErr <- nil
	testControllerOptions.XDSUpdater = fx
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)
	message := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)

	steps := []struct {
		description string
		annotations map[string]string
		want        string
	}{
		{
			description: "no annotation",
			annotations: map[string]string{},
			want:        "ConfigUpdate",
		},
		{
			description: "service annotation only",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion": "1",
			},
			want: "ConfigUpdate",
		},
		{
			description: "service and endpoints annotation only service version changed",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion":   "2",
				"networking.alpha.istio.io/endpointsVersion": "1",
			},
			want: "ConfigUpdate",
		},
		{
			description: "service and endpoints annotation only endpoints version changed",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion":   "2",
				"networking.alpha.istio.io/endpointsVersion": "2",
			},
			want: "EDSUpdate",
		},
		{
			description: "service and endpoints annotation both versions changed",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion":   "3",
				"networking.alpha.istio.io/endpointsVersion": "3",
			},
			want: "ConfigUpdate",
		},
	}
	for i, s := range steps {
		t.Run(fmt.Sprintf("non incremental resource with %s", s.description), func(_ *testing.T) {
			change := convertToChange([]proto.Message{message},
				[]string{"random-namespace/test-synthetic-se"},
				setVersion(strconv.Itoa(i)),
				setAnnotations(s.annotations),
				setCollection(schemas.SyntheticServiceEntry.Collection),
				setTypeURL(schemas.SyntheticServiceEntry.MessageName))

			err := controller.Apply(change)
			g.Expect(err).ToNot(gomega.HaveOccurred())

			g.Expect(len(fx.Events)).To(gomega.Equal(1))
			update := <-fx.Events
			g.Expect(update).To(gomega.Equal(s.want))
		})
	}
}

func TestApplyIncrementalChangeRemove(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message0 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message0},
		[]string{"random-namespace/test-synthetic-se"},
		setVersion("1"),
		setIncremental(),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	message1 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry1)
	change = convertToChange([]proto.Message{message1},
		[]string{"random-namespace/test-synthetic-se1"},
		setVersion("1"),
		setIncremental(),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(2))

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	for _, se := range entries {
		g.Expect(se.Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
		switch se.Name {
		case "test-synthetic-se":
			g.Expect(se.Spec).To(gomega.Equal(message0))
		case "test-synthetic-se1":
			g.Expect(se.Spec).To(gomega.Equal(message1))
		}
	}

	change = convertToChange([]proto.Message{message1},
		[]string{"random-namespace/test-synthetic-se1"},
		setVersion("1"),
		setIncremental(),
		setRemoved([]string{"random-namespace/test-synthetic-se"}),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se1"))
	g.Expect(entries[0].Spec).To(gomega.Equal(message1))

	// We do not expect to call either EDSUpdate or configUpdate
	// since we have already done that for the old config
	g.Expect(len(fx.Events)).To(gomega.Equal(0))
}

func TestApplyIncrementalChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message0 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message0},
		[]string{"random-namespace/test-synthetic-se"},
		setVersion("1"),
		setIncremental(),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	message1 := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry1)
	change = convertToChange([]proto.Message{message1},
		[]string{"random-namespace/test-synthetic-se1"},
		setVersion("1"),
		setIncremental(),
		setCollection(schemas.SyntheticServiceEntry.Collection),
		setTypeURL(schemas.SyntheticServiceEntry.MessageName))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(schemas.SyntheticServiceEntry.Type, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(2))

	for _, se := range entries {
		g.Expect(se.Type).To(gomega.Equal(schemas.SyntheticServiceEntry.Type))
		switch se.Name {
		case "test-synthetic-se":
			g.Expect(se.Spec).To(gomega.Equal(message0))
		case "test-synthetic-se1":
			g.Expect(se.Spec).To(gomega.Equal(message1))
		}
	}

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))
}

func TestApplyIncrementalChangesAnnotations(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/17814")
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	fx.EDSErr <- nil
	testControllerOptions.XDSUpdater = fx
	options := &coredatamodel.DiscoveryOptions{}
	d := coredatamodel.NewMCPDiscovery(options)
	controller := coredatamodel.NewSyntheticServiceEntryController(testControllerOptions, d)

	message := convertToResource(g, schemas.SyntheticServiceEntry.MessageName, syntheticServiceEntry0)

	steps := []struct {
		description string
		annotations map[string]string
		want        string
	}{
		{
			description: "no annotations",
			annotations: map[string]string{},
			want:        "ConfigUpdate",
		},
		{
			description: "service annotation only",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion": "1",
			},
			want: "ConfigUpdate",
		},
		{
			description: "service and endpoints annotation only service version changed",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion":   "2",
				"networking.alpha.istio.io/endpointsVersion": "1",
			},
			want: "ConfigUpdate",
		},
		{
			description: "service and endpoints annotation only endpoints version changed",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion":   "2",
				"networking.alpha.istio.io/endpointsVersion": "2",
			},
			want: "EDSUpdate",
		},
		{
			description: "service and endpoints annotation both versions changed",
			annotations: map[string]string{
				"networking.alpha.istio.io/serviceVersion":   "3",
				"networking.alpha.istio.io/endpointsVersion": "3",
			},
			want: "ConfigUpdate",
		},
	}
	for i, s := range steps {
		t.Run(fmt.Sprintf("incrementall resource with %s", s.description), func(_ *testing.T) {
			change := convertToChange([]proto.Message{message},
				[]string{"random-namespace/test-synthetic-se"},
				setVersion(strconv.Itoa(i)),
				setIncremental(),
				setAnnotations(s.annotations),
				setCollection(schemas.SyntheticServiceEntry.Collection),
				setTypeURL(schemas.SyntheticServiceEntry.MessageName))

			err := controller.Apply(change)
			g.Expect(err).ToNot(gomega.HaveOccurred())

			g.Expect(len(fx.Events)).To(gomega.Equal(1))
			update := <-fx.Events
			g.Expect(update).To(gomega.Equal(s.want))
		})
	}
}
