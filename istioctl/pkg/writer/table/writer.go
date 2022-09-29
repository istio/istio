// Copyright Istio Authors
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

package table

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
)

type ColoredTableWriter struct {
	writer     io.Writer
	header     Row
	rows       []Row
	addRowFunc func(obj interface{}) Row
}

func NewStyleWriter(writer io.Writer) *ColoredTableWriter {
	return &ColoredTableWriter{
		writer: writer,
		rows:   make([]Row, 0),
		header: Row{},
	}
}

type BuildRowFunc func(obj interface{}) Row

type Cell struct {
	Value      string
	Attributes []color.Attribute
}

type Row struct {
	Cells []Cell
}

func NewCell(value string, attributes ...color.Attribute) Cell {
	attrs := append([]color.Attribute{}, attributes...)
	return Cell{value, attrs}
}

func (cell Cell) String() string {
	if len(cell.Attributes) == 0 {
		return cell.Value
	}
	s := color.New(cell.Attributes...)
	s.EnableColor()
	return s.Sprintf("%s", cell.Value)
}

func (c *ColoredTableWriter) getTableOutput(allRows []Row) [][]Cell {
	output := [][]Cell{}
	if len(c.header.Cells) != 0 {
		output = append(output, c.header.Cells)
	}
	for _, row := range allRows {
		output = append(output, row.Cells)
	}
	return output
}

func (c *ColoredTableWriter) SetAddRowFunc(f func(obj interface{}) Row) {
	c.addRowFunc = f
}

func (c *ColoredTableWriter) AddHeader(names ...string) {
	cells := make([]Cell, 0)
	for _, name := range names {
		cells = append(cells, NewCell(name))
	}
	c.header = Row{Cells: cells}
}

func (c *ColoredTableWriter) AddRow(obj interface{}) {
	c.rows = append(c.rows, c.addRowFunc(obj))
}

func (c *ColoredTableWriter) Flush() {
	output := c.getTableOutput(c.rows)
	if len(output) == 0 {
		return
	}
	sep := getMaxWidths(output)
	for _, row := range output {
		for i, col := range row {
			_, _ = fmt.Fprint(c.writer, col.String())
			if i == len(row)-1 {
				_, _ = fmt.Fprint(c.writer, "\n")
			} else {
				padAmount := sep[i] - utf8.RuneCount([]byte(col.Value)) + 2
				_, _ = fmt.Fprint(c.writer, strings.Repeat(" ", padAmount))
			}
		}
	}
}

func max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

func getMaxWidths(output [][]Cell) []int {
	widths := make([]int, len(output[0]))
	for _, row := range output {
		for i, col := range row {
			widths[i] = max(widths[i], len(col.String()))
		}
	}
	return widths
}
