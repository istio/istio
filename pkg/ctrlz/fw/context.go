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

package fw

import (
	"html/template"

	"github.com/gorilla/mux"
)

// Topic is used to describe a single major ControlZ functional area.
type Topic interface {

	// Title returns the title for the area, which will be used in the sidenav and window title.
	Title() string

	// Prefix is the name used to reference this functionality in URLs.
	Prefix() string

	// Activate triggers a topic to register itself to receive traffic.
	Activate(TopicContext)
}

// TopicContext provides support objects needed to register a topic.
type TopicContext interface {
	// HTMLRouter is used to control HTML traffic delivered to this topic.
	HTMLRouter() *mux.Router

	// JSONRouter is used to control HTML traffic delivered to this topic.
	JSONRouter() *mux.Router

	// Layout is the template used as the primary layout for the topic's HTML content.
	Layout() *template.Template
}

type context struct {
	htmlRouter *mux.Router
	jsonRouter *mux.Router
	layout     *template.Template
}

// NewContext creates a new TopicContext.
func NewContext(htmlRouter *mux.Router, jsonRouter *mux.Router, layout *template.Template) TopicContext {
	return context{
		htmlRouter: htmlRouter,
		jsonRouter: jsonRouter,
		layout:     layout,
	}
}

func (c context) HTMLRouter() *mux.Router {
	return c.htmlRouter
}

func (c context) JSONRouter() *mux.Router {
	return c.jsonRouter
}

func (c context) Layout() *template.Template {
	return c.layout
}
