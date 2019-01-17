//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package configz

import (
	"html/template"
	"net/http"

	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/ctrlz/fw"
	"istio.io/istio/pkg/mcp/sink"
)

// configzTopic topic is a Topic fw.implementation that exposes the state info about an MCP sink.
type configzTopic struct {
	tmpl *template.Template

	topic SinkTopic
}

var _ fw.Topic = &configzTopic{}

// SinkTopic defines the expected interface for producing configz data from an MCP sink.
type SinkTopic interface {
	SnapshotRequestInfo() []sink.RecentRequestInfo
	Metadata() map[string]string
	ID() string
	Collections() []string
}

// Register the Configz topic for the given sink.
// TODO: Multi-client registration is currently not supported. We should update the topic, so that we can
// show output from multiple clients.
func Register(topic SinkTopic) {
	ctrlz.RegisterTopic(CreateTopic(topic))
}

// CreateTopic creates and returns a configz topic from the given MCP client. It does not do any registration.
func CreateTopic(topic SinkTopic) fw.Topic {
	return &configzTopic{
		topic: topic,
	}
}

// Title is implementation of Topic.Title.
func (c *configzTopic) Title() string {
	return "Config"
}

// Prefix is implementation of Topic.Prefix.
func (c *configzTopic) Prefix() string {
	return "config"
}

type data struct {
	ID          string
	Metadata    map[string]string
	Collections []string

	LatestRequests []sink.RecentRequestInfo
}

// Activate is implementation of Topic.Activate.
func (c *configzTopic) Activate(context fw.TopicContext) {
	l := template.Must(context.Layout().Clone())
	c.tmpl = template.Must(l.Parse(string(MustAsset("assets/templates/config.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		d := c.collectData()
		fw.RenderHTML(w, c.tmpl, d)
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		d := c.collectData()
		fw.RenderJSON(w, http.StatusOK, d)
	})
}

func (c *configzTopic) collectData() *data {
	return &data{
		ID:             c.topic.ID(),
		Metadata:       c.topic.Metadata(),
		Collections:    c.topic.Collections(),
		LatestRequests: c.topic.SnapshotRequestInfo(),
	}
}
