// Copyright 2018 Istio Authors. All Rights Reserved.
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

package checker

import (
	"fmt"
	"go/token"
	"sort"
)

// Item stores error position in a test and error message.
type Item struct {
	pos int
	msg string
}

// ItemList is an array of items.
type ItemList []Item

// Report populates lint report.
type Report struct {
	items ItemList
}

// NewLintReport creates and returns a Report object.
func NewLintReport() *Report {
	return &Report{}
}

// Items returns formatted report as a string slice.
func (lr *Report) Items() []string {
	itemStr := []string{}
	for _, it := range lr.items {
		itemStr = append(itemStr, it.msg)
	}
	return itemStr
}

// Sort sorts report items.
func (lr *Report) Sort() {
	sort.Sort(&lr.items)
}

func (l ItemList) Len() int           { return len(l) }
func (l ItemList) Less(i, j int) bool { return l[i].pos < l[j].pos }
func (l ItemList) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

// AddItem creates a new lint error report.
func (lr *Report) AddItem(pos token.Position, id string, msg string) {
	item := Item{
		pos: pos.Line,
		msg: fmt.Sprintf("%v:%v:%v:%s (%s)",
			pos.Filename,
			pos.Line,
			pos.Column,
			msg,
			id),
	}
	lr.items = append(lr.items, item)
}

// AddString creates a new string line in report.
func (lr *Report) AddString(msg string) {
	itm := Item{
		pos: 0,
		msg: msg,
	}
	lr.items = append(lr.items, itm)
}
