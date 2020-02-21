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

package serviceentry_test

import (
	"fmt"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"

	"istio.io/api/annotation"
	mcpapi "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/synthetic/serviceentry"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/mcp/sink"
)

var (
	sseCollection = collections.IstioNetworkingV1Alpha3SyntheticServiceentries.Name().String()
	sseKind       = collections.IstioNetworkingV1Alpha3SyntheticServiceentries.Resource().GroupVersionKind()
	sseProto      = collections.IstioNetworkingV1Alpha3SyntheticServiceentries.Resource().Proto()

	gateway = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Number:   443,
					Name:     "https",
					Protocol: "HTTP",
				},
				Hosts: []string{"*.secure.example.com"},
			},
		},
	}

	serviceEntry = &networking.ServiceEntry{
		Hosts: []string{"example.com"},
		Ports: []*networking.Port{
			{
				Name:     "http",
				Number:   7878,
				Protocol: "http",
			},
		},
		Location:   networking.ServiceEntry_MESH_INTERNAL,
		Resolution: networking.ServiceEntry_STATIC,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "127.0.0.1",
				Ports: map[string]uint32{
					"http": 4433,
				},
				Labels: map[string]string{"label": "random-label"},
			},
		},
	}

	syntheticServiceEntry0 = &networking.ServiceEntry{
		Hosts: []string{"svc.example2.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "2.2.2.2",
				Ports:   map[string]uint32{"http-port": 7080, "http-alt-port": 18080},
			},
			{
				Address: "3.3.3.3",
				Ports:   map[string]uint32{"http-port": 1080},
			},
			{
				Address: "4.4.4.4",
				Ports:   map[string]uint32{"http-port": 1080},
				Labels:  map[string]string{"foo": "bar"},
			},
		},
	}

	syntheticServiceEntry1 = &networking.ServiceEntry{
		Hosts: []string{"example2.com"},
		Ports: []*networking.Port{
			{Number: 80, Name: "http-port", Protocol: "http"},
			{Number: 8080, Name: "http-alt-port", Protocol: "http"},
		},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
		Endpoints: []*networking.ServiceEntry_Endpoint{
			{
				Address: "2.2.2.2",
				Ports:   map[string]uint32{"http-port": 9080, "http-alt-port": 18081},
			},
			{
				Address: "3.3.3.3",
				Ports:   map[string]uint32{"http-port": 1080},
			},
			{
				Address: "5.5.5.5",
				Ports:   map[string]uint32{"http-port": 1081},
				Labels:  map[string]string{"foo1": "bar1"},
			},
		},
	}

	testControllerOptions = &serviceentry.Options{
		DomainSuffix: "cluster.local",
	}
)

func TestIncrementalControllerHasSynced(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)
	g.Expect(controller.HasSynced()).To(gomega.BeFalse())

	for i, se := range []*networking.ServiceEntry{syntheticServiceEntry0, syntheticServiceEntry1} {
		message := convertToResource(g, sseProto, se)

		change := convertToChange([]proto.Message{message},
			[]string{fmt.Sprintf("random-namespace/test-synthetic-se-%d", i)},
			setCollection(sseCollection),
			setTypeURL(sseProto))

		err := controller.Apply(change)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(controller.HasSynced()).To(gomega.BeTrue())
	}
}

func TestIncrementalControllerConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	schemas := controller.Schemas()
	g.Expect(schemas.Kinds()).To(gomega.HaveLen(1))
	g.Expect(schemas.Kinds()).To(gomega.ContainElement(collections.IstioNetworkingV1Alpha3SyntheticServiceentries.Resource().Kind()))
}

func TestIncrementalControllerListInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	c, err := controller.List(collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind(), "some-phony-name-space")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("list unknown type networking.istio.io/v1alpha3/Gateway"))
}

func TestIncrementalControllerListCorrectTypeNoData(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	c, err := controller.List(sseKind, "some-phony-name-space")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).ToNot(gomega.HaveOccurred())
}

func TestIncrementalControllerListAllNameSpace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	syntheticServiceEntry2 := proto.Clone(syntheticServiceEntry1).(*networking.ServiceEntry)
	syntheticServiceEntry2.Ports = serviceEntry.Ports
	syntheticServiceEntry2.Endpoints = serviceEntry.Endpoints

	messages := convertToResources(g,
		sseProto,
		[]proto.Message{syntheticServiceEntry0, syntheticServiceEntry1, syntheticServiceEntry2})

	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"default/sse-0", "namespace2/sse-1", "namespace3/sse-2"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(3))

	for _, conf := range c {
		g.Expect(conf.GroupVersionKind()).To(gomega.Equal(sseKind))
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
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	syntheticServiceEntry2 := proto.Clone(syntheticServiceEntry1).(*networking.ServiceEntry)
	syntheticServiceEntry2.Ports = serviceEntry.Ports
	syntheticServiceEntry2.Endpoints = serviceEntry.Endpoints

	messages := convertToResources(g,
		sseProto,
		[]proto.Message{syntheticServiceEntry0, syntheticServiceEntry1, syntheticServiceEntry2})

	message, message2, message3 := messages[0], messages[1], messages[2]
	change := convertToChange(
		[]proto.Message{message, message2, message3},
		[]string{"default/sse-0", "namespace2/sse-1", "namespace2/sse-2"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(sseKind, "default")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("sse-0"))
	g.Expect(c[0].GroupVersionKind()).To(gomega.Equal(sseKind))
	g.Expect(c[0].Namespace).To(gomega.Equal("default"))
	g.Expect(c[0].Spec).To(gomega.Equal(message))

	c, err = controller.List(sseKind, "namespace2")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(2))
	for _, conf := range c {
		g.Expect(conf.GroupVersionKind()).To(gomega.Equal(sseKind))
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
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	message := convertToResource(g,
		collections.IstioNetworkingV1Alpha3Gateways.Resource().Proto(),
		gateway)

	change := convertToChange(
		[]proto.Message{message},
		[]string{"some-gateway"},
		setCollection(collections.IstioNetworkingV1Alpha3Gateways.Name().String()),
		setTypeURL(collections.IstioNetworkingV1Alpha3Gateways.Resource().Kind()))

	err := controller.Apply(change)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring(fmt.Sprintf("apply: type not supported %s",
		collections.IstioNetworkingV1Alpha3Gateways.Name().String())))
}

func TestIncrementalControllerApplyMetadataNameIncludesNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	message := convertToResource(g, sseProto, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-synthetic-se"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(sseKind, "random-namespace")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("test-synthetic-se"))
	g.Expect(c[0].GroupVersionKind()).To(gomega.Equal(sseKind))
	g.Expect(c[0].Spec).To(gomega.Equal(message))
}

func TestIncrementalControllerApplyMetadataNameWithoutNamespace(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	fx.EDSErr <- nil
	testControllerOptions.XDSUpdater = fx
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	message0 := convertToResource(g, sseProto, syntheticServiceEntry0)
	change0 := convertToChange([]proto.Message{message0},
		[]string{"synthetic-se-0"},
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change0)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	message1 := convertToResource(g, sseProto, syntheticServiceEntry1)
	change1 := convertToChange([]proto.Message{message1},
		[]string{"synthetic-se-1"},
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err = controller.Apply(change1)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(2))
	for _, se := range c {
		g.Expect(se.GroupVersionKind()).To(gomega.Equal(sseKind))
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
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	message := convertToResource(g, sseProto, syntheticServiceEntry0)
	change := convertToChange([]proto.Message{message},
		[]string{"synthetic-se-0"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	g.Expect(c[0].Name).To(gomega.Equal("synthetic-se-0"))
	g.Expect(c[0].GroupVersionKind()).To(gomega.Equal(sseKind))
	g.Expect(c[0].Spec).To(gomega.Equal(message))

	change = convertToChange([]proto.Message{},
		[]string{"some-synthetic-se"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	c, err = controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(c)).To(gomega.Equal(1))
	// still expecting the old config
	g.Expect(c[0].Name).To(gomega.Equal("synthetic-se-0"))
	g.Expect(c[0].GroupVersionKind()).To(gomega.Equal(sseKind))
	g.Expect(c[0].Spec).To(gomega.Equal(message))
}

func TestIncrementalControllerApplyInvalidResource(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	se := proto.Clone(syntheticServiceEntry1).(*networking.ServiceEntry)
	se.Hosts = nil

	message0 := convertToResource(g, sseProto, se)

	change := convertToChange(
		[]proto.Message{message0},
		[]string{"bar-namespace/foo"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
}

func TestIncrementalControllerApplyInvalidResource_BadTimestamp(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)

	message0 := convertToResource(g, sseProto, syntheticServiceEntry0)
	change := convertToChange(
		[]proto.Message{message0},
		[]string{"bar-namespace/foo"},
		setCollection(sseCollection),
		setTypeURL(sseProto))
	change.Objects[0].Metadata.CreateTime = &types.Timestamp{
		Seconds: -1,
		Nanos:   -1,
	}

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(0))
}

func TestApplyNonIncrementalChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)
	controller.RegisterEventHandler(sseKind, func(model.Config, model.Config, model.Event) {
		fx.ConfigUpdate(nil)
	})

	message := convertToResource(g, sseProto, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-synthetic-se"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	change = convertToChange([]proto.Message{message},
		[]string{"random-namespace/test-synthetic-se1"},
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se1"))

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))
}

func TestApplyNonIncrementalAnnotations(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	fx.EDSErr <- nil
	testControllerOptions.XDSUpdater = fx
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)
	controller.RegisterEventHandler(sseKind, func(model.Config, model.Config, model.Event) {
		fx.ConfigUpdate(nil)
	})
	message := convertToResource(g, sseProto, syntheticServiceEntry0)

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
			want: "ConfigUpdate",
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
	for _, s := range steps {
		t.Run(fmt.Sprintf("non incremental resource with %s", s.description), func(_ *testing.T) {
			change := convertToChange([]proto.Message{message},
				[]string{"random-namespace/test-synthetic-se"},
				setAnnotations(s.annotations),
				setCollection(sseCollection),
				setTypeURL(sseProto))

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
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)
	controller.RegisterEventHandler(sseKind, func(model.Config, model.Config, model.Event) {
		fx.ConfigUpdate(nil)
	})

	message0 := convertToResource(g, sseProto, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message0},
		[]string{"random-namespace/test-synthetic-se"},
		setAnnotations(map[string]string{
			annotation.AlphaNetworkingServiceVersion.Name: "1",
		}),
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	message1 := convertToResource(g, sseProto, syntheticServiceEntry1)
	change = convertToChange([]proto.Message{message1},
		[]string{"random-namespace/test-synthetic-se1"},
		setAnnotations(map[string]string{
			annotation.AlphaNetworkingServiceVersion.Name: "1",
		}),
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(2))

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	for _, se := range entries {
		g.Expect(se.GroupVersionKind()).To(gomega.Equal(sseKind))
		switch se.Name {
		case "test-synthetic-se":
			g.Expect(se.Spec).To(gomega.Equal(message0))
		case "test-synthetic-se1":
			g.Expect(se.Spec).To(gomega.Equal(message1))
		}
	}

	// add a new resource and remove an old resource
	change = convertToChange([]proto.Message{message1},
		[]string{"random-namespace/test-synthetic-se2"},
		setIncremental(),
		setAnnotations(map[string]string{
			annotation.AlphaNetworkingServiceVersion.Name: "1",
		}),
		setRemoved([]string{"random-namespace/test-synthetic-se"}),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	entries, err = controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(2))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se1"))
	g.Expect(entries[0].Spec).To(gomega.Equal(message1))
	g.Expect(entries[1].Name).To(gomega.Equal("test-synthetic-se2"))
	g.Expect(entries[1].Spec).To(gomega.Equal(message1))

	// only remove an old resource, should trigger a push
	change = convertToChange(nil,
		nil,
		setIncremental(),
		setAnnotations(map[string]string{
			annotation.AlphaNetworkingServiceVersion.Name: "1",
		}),
		setRemoved([]string{"random-namespace/test-synthetic-se2"}),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	update = <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))
	entries, err = controller.List(sseKind, "")
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
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)
	controller.RegisterEventHandler(sseKind, func(model.Config, model.Config, model.Event) {
		fx.ConfigUpdate(nil)
	})

	message0 := convertToResource(g, sseProto, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message0},
		[]string{"random-namespace/test-synthetic-se"},
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	message1 := convertToResource(g, sseProto, syntheticServiceEntry1)
	change = convertToChange([]proto.Message{message1},
		[]string{"random-namespace/test-synthetic-se1"},
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(2))

	for _, se := range entries {
		g.Expect(se.GroupVersionKind()).To(gomega.Equal(sseKind))
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

func TestApplyIncrementalChangeEndpiontVersionWithoutServiceVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	testControllerOptions.XDSUpdater = fx
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)
	controller.RegisterEventHandler(sseKind, func(model.Config, model.Config, model.Event) {
		fx.ConfigUpdate(nil)
	})

	message0 := convertToResource(g, sseProto, syntheticServiceEntry0)

	change := convertToChange([]proto.Message{message0},
		[]string{"random-namespace/test-synthetic-se"},
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	err := controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err := controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update := <-fx.Events
	g.Expect(update).To(gomega.Equal("ConfigUpdate"))

	change = convertToChange([]proto.Message{message0},
		[]string{"random-namespace/test-synthetic-se"},
		setAnnotations(map[string]string{
			annotation.AlphaNetworkingEndpointsVersion.Name: "1",
		}),
		setIncremental(),
		setCollection(sseCollection),
		setTypeURL(sseProto))

	fx.EDSErr <- nil
	err = controller.Apply(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	entries, err = controller.List(sseKind, "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(entries).To(gomega.HaveLen(1))
	g.Expect(entries[0].Name).To(gomega.Equal("test-synthetic-se"))

	update = <-fx.Events
	t.Logf("len() %d", len(fx.Events))
	g.Expect(update).To(gomega.Equal("EDSUpdate"))

}

func TestApplyIncrementalChangesAnnotations(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	fx := NewFakeXDS()
	fx.EDSErr <- nil
	testControllerOptions.XDSUpdater = fx
	controller := serviceentry.NewSyntheticServiceEntryController(testControllerOptions)
	controller.RegisterEventHandler(sseKind, func(model.Config, model.Config, model.Event) {
		fx.ConfigUpdate(nil)
	})
	message := convertToResource(g, sseProto, syntheticServiceEntry0)

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
			description: "endpoint annotation only",
			annotations: map[string]string{
				"networking.alpha.istio.io/endpointsVersion": "1",
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
	for _, s := range steps {
		t.Run(fmt.Sprintf("incrementall resource with %s", s.description), func(_ *testing.T) {
			change := convertToChange([]proto.Message{message},
				[]string{"random-namespace/test-synthetic-se"},
				setIncremental(),
				setAnnotations(s.annotations),
				setCollection(sseCollection),
				setTypeURL(sseProto))

			err := controller.Apply(change)
			g.Expect(err).ToNot(gomega.HaveOccurred())

			g.Expect(len(fx.Events)).To(gomega.Equal(1))
			update := <-fx.Events
			g.Expect(update).To(gomega.Equal(s.want))
		})
	}
}

func convertToChange(resources []proto.Message, names []string, options ...func(*sink.Change)) *sink.Change {
	out := new(sink.Change)
	for i, res := range resources {
		obj := &sink.Object{
			Metadata: &mcpapi.Metadata{
				Name: names[i],
			},
			Body: res,
		}
		out.Objects = append(out.Objects, obj)
	}
	// apply options
	for _, option := range options {
		option(out)
	}
	return out
}

func setIncremental() func(*sink.Change) {
	return func(c *sink.Change) {
		c.Incremental = true
	}
}

func setRemoved(removed []string) func(*sink.Change) {
	return func(c *sink.Change) {
		c.Removed = removed
	}
}

func setCollection(collection string) func(*sink.Change) {
	return func(c *sink.Change) {
		c.Collection = collection
	}
}

func setAnnotations(a map[string]string) func(*sink.Change) {
	return func(c *sink.Change) {
		for _, obj := range c.Objects {
			obj.Metadata.Annotations = a
		}
	}
}

func setTypeURL(url string) func(*sink.Change) {
	return func(c *sink.Change) {
		for _, obj := range c.Objects {
			obj.TypeURL = url
		}
	}
}

func convertToResource(g *gomega.GomegaWithT, typeURL string, resource proto.Message) (messages proto.Message) {
	marshaled, err := proto.Marshal(resource)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	message, err := makeMessage(marshaled, typeURL)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	return message
}

func convertToResources(g *gomega.GomegaWithT, typeURL string, resources []proto.Message) (messages []proto.Message) {
	for _, resource := range resources {
		message := convertToResource(g, typeURL, resource)
		messages = append(messages, message)
	}
	return messages
}

func makeMessage(value []byte, typeURL string) (proto.Message, error) {
	resource := &types.Any{
		TypeUrl: fmt.Sprintf("type.googleapis.com/%s", typeURL),
		Value:   value,
	}

	var dynamicAny types.DynamicAny
	err := types.UnmarshalAny(resource, &dynamicAny)
	if err == nil {
		return dynamicAny.Message, nil
	}

	return nil, err
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

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

func (f *FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {
	f.Events <- "ConfigUpdate"
}

func (f *FakeXdsUpdater) EDSUpdate(_, _, _ string, entry []*model.IstioEndpoint) error {
	f.Events <- "EDSUpdate"
	f.Endpoints <- entry
	return <-f.EDSErr
}

func (f *FakeXdsUpdater) SvcUpdate(_, _ string, _ string, _ model.Event) {
}

func (f *FakeXdsUpdater) ProxyUpdate(_, _ string) {
}
