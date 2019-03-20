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

package topics

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v2"

	"istio.io/istio/pkg/ctrlz/fw"
)

// ReadableCollection is a staticCollection collection of entries to be exposed via CtrlZ.
type ReadableCollection interface {
	Name() string
	Keys() (keys []string, err error)
	Get(id string) (interface{}, error)
}

// collection topic is a Topic fw.implementation that exposes a set of collections through CtrlZ.
type collectionTopic struct {
	title       string
	prefix      string
	collections []ReadableCollection

	mainTmpl *template.Template
	listTmpl *template.Template
	itemTmpl *template.Template
}

var _ fw.Topic = &collectionTopic{}

// Title is implementation of Topic.Title.
func (c *collectionTopic) Title() string {
	return c.title
}

// Prefix is implementation of Topic.Prefix.
func (c *collectionTopic) Prefix() string {
	return c.prefix
}

// Activate is implementation of Topic.Activate.
func (c *collectionTopic) Activate(context fw.TopicContext) {

	l := template.Must(context.Layout().Clone())
	c.mainTmpl = template.Must(l.Parse(string(MustAsset("assets/templates/collection/main.html"))))

	l = template.Must(context.Layout().Clone())
	c.listTmpl = template.Must(l.Parse(string(MustAsset("assets/templates/collection/list.html"))))

	l = template.Must(context.Layout().Clone())
	c.itemTmpl = template.Must(l.Parse(string(MustAsset("assets/templates/collection/item.html"))))

	_ = context.HTMLRouter().
		StrictSlash(true).
		NewRoute().
		PathPrefix("/").
		Methods("GET").
		HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

			parts := strings.SplitN(req.URL.Path, "/", 4)
			parts = parts[2:] // Skip the empty and title parts.

			switch len(parts) {
			case 1:
				if parts[0] == "" {
					c.handleMain(w, req)
				} else {
					c.handleCollection(w, req, parts[0])
				}

			case 2:
				c.handleItem(w, req, parts[0], parts[1])

			default:
				c.handleError(w, req, fmt.Sprintf("InvalidUrl %s", req.URL.Path))
			}
		})
}

// mainContext is passed to the template processor and carries information that is used by the main template.
type mainContext struct {
	Title       string
	Collections []string
	Error       string
}

func (c *collectionTopic) handleMain(w http.ResponseWriter, _ *http.Request) {
	context := mainContext{}
	var names []string
	for _, n := range c.collections {
		names = append(names, n.Name())
	}
	sort.Strings(names)
	context.Collections = names
	context.Title = c.title
	fw.RenderHTML(w, c.mainTmpl, context)
}

// listContext is passed to the template processor and carries information that is used by the list template.
type listContext struct {
	Collection string
	Keys       []string
	Error      string
}

func (c *collectionTopic) handleCollection(w http.ResponseWriter, _ *http.Request, collection string) {
	k, err := c.listCollection(collection)
	context := listContext{}
	if err == nil {
		context.Collection = collection
		context.Keys = k
	} else {
		context.Error = err.Error()
	}
	fw.RenderHTML(w, c.listTmpl, context)
}

// itemContext is passed to the template processor and carries information that is used by the list template.
type itemContext struct {
	Collection string
	Key        string
	Value      interface{}
	Error      string
}

func (c *collectionTopic) handleItem(w http.ResponseWriter, _ *http.Request, collection, key string) {
	v, err := c.getItem(collection, key)
	context := itemContext{}
	if err == nil {
		switch v.(type) {
		case string:

		default:
			var b []byte
			if b, err = yaml.Marshal(v); err != nil {
				context.Error = err.Error()
				break
			}
			v = string(b)
		}

		context.Collection = collection
		context.Key = key
		context.Value = v
	} else {
		context.Error = err.Error()
	}
	fw.RenderHTML(w, c.itemTmpl, context)
}

func (c *collectionTopic) handleError(w http.ResponseWriter, _ *http.Request, error string) {
	fw.RenderHTML(w, c.mainTmpl, mainContext{Error: error})
}

func (c *collectionTopic) listCollection(name string) ([]string, error) {
	for _, col := range c.collections {
		if col.Name() == name {
			return col.Keys()
		}
	}

	return nil, fmt.Errorf("collection not found: %q", name)
}

func (c *collectionTopic) getItem(collection string, id string) (interface{}, error) {
	for _, col := range c.collections {
		if col.Name() == collection {
			return col.Get(id)
		}
	}

	return nil, fmt.Errorf("collection not found: %q", collection)
}

// NewCollectionTopic creates a new custom topic that exposes the provided collections.
func NewCollectionTopic(title string, prefix string, collections ...ReadableCollection) fw.Topic {
	return &collectionTopic{
		title:       title,
		prefix:      prefix,
		collections: collections,
	}
}

// NewStaticCollection creates a static collection from the given set of items.
func NewStaticCollection(name string, items map[string]interface{}) ReadableCollection {
	return &staticCollection{
		name:  name,
		items: items,
	}
}

// staticCollection is a ReadableCollection implementation that operates on static data that is supplied
// during construction.
type staticCollection struct {
	name  string
	items map[string]interface{}
}

var _ ReadableCollection = &staticCollection{}

// Name is implementation of ReadableCollection.Name.
func (r *staticCollection) Name() string {
	return r.name
}

// Keys is implementation of ReadableCollection.Keys.
func (r *staticCollection) Keys() ([]string, error) {
	var keys []string
	for k := range r.items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys, nil
}

// Get is implementation of ReadableCollection.Get.
func (r *staticCollection) Get(id string) (interface{}, error) {
	return r.items[id], nil
}
