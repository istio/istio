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

package analyzer

import "fmt"

type Level int

const (
	Info    Level = iota
	Warning
	Error
)

type Message struct {
	Level Level
	Content string
	Source string
	Code int
}

func (m *Message) String() string {
	src := ""
	if m.Source != "" {
		src = "  " + m.Source
	}

	lvl := "?"
	switch m.Level {
	case Info:
		lvl = "I"
	case Warning:
		lvl = "W"
	case Error:
		lvl = "E"
	}

	return fmt.Sprintf("[%s%#04d] %s%s", lvl, m.Code, m.Content, src)
}

