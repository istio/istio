//  Copyright 2019 Istio Authors
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
	"istio.io/istio/pkg/mcp/configz/server/assets"
	"istio.io/istio/pkg/mcp/snapshot"
)

// configzTopic topic is a Topic fw.implementation that exposes the state info for different snapshots galley is serving.
type configzTopic struct {
	tmpl *template.Template

	topic SnapshotTopic
}

var _ fw.Topic = &configzTopic{}

// SnapshotTopic defines the expected interface for producing configz data from MCP snapshots.
type SnapshotTopic interface {
	GetSnapshotInfo(group string) []snapshot.Info
	GetGroups() []string
}

// Register the Configz topic for the snapshots.
func Register(topic SnapshotTopic) {
	ctrlz.RegisterTopic(CreateTopic(topic))
}

// CreateTopic creates and returns a configz topic for the snapshots. It does not do any registration.
func CreateTopic(topic SnapshotTopic) fw.Topic {
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
	Snapshots []snapshot.Info
	Groups    []string
}

// Activate is implementation of Topic.Activate.
func (c *configzTopic) Activate(context fw.TopicContext) {
	l := template.Must(context.Layout().Clone())
	c.tmpl = template.Must(l.Parse(string(assets.MustAsset("templates/config.html"))))

	_ = context.HTMLRouter().StrictSlash(true).NewRoute().Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		d := c.collectData("")
		fw.RenderHTML(w, c.tmpl, d)
	})

	_ = context.JSONRouter().StrictSlash(true).NewRoute().Methods("GET").Path("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		keys, ok := req.URL.Query()["group"]
		group := ""
		if ok && len(keys[0]) > 1 {
			group = keys[0]
		}
		d := c.collectData(group)
		fw.RenderJSON(w, http.StatusOK, d)
	})
}

func (c *configzTopic) collectData(group string) *data {
	return &data{
		Snapshots: c.topic.GetSnapshotInfo(group),
		Groups:    c.topic.GetGroups(),
	}
}
