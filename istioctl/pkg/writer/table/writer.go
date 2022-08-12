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

	"github.com/logrusorgru/aurora"
)

type coloredTableWriter struct {
	writer     io.Writer
	header     Row
	rows       []Row
	addRowFunc func(obj interface{}) Row
}

func NewStyleWriter(writer io.Writer) *coloredTableWriter {
	return &coloredTableWriter{
		writer: writer,
		rows:   make([]Row, 0),
		header: Row{},
	}
}

type StyleFunc func(arg interface{}) aurora.Value

type BuildRowFunc func(obj interface{}) Row

type Cell struct {
	Value  string
	Styles []StyleFunc
}

type Row struct {
	Cells []Cell
}

func NewCell(value string, styles ...StyleFunc) Cell {
	filtered := make([]StyleFunc, 0)
	for _, s := range styles {
		if s != nil {
			filtered = append(filtered, s)
		}
	}
	return Cell{value, filtered}
}

func (cell Cell) String() string {
	out := aurora.Reset(cell.Value)
	for _, s := range cell.Styles {
		out = s(out)
	}
	return out.String()
}

func (c *coloredTableWriter) getTableOutput(allRows []Row) [][]Cell {
	output := [][]Cell{}
	if len(c.header.Cells) != 0 {
		output = append(output, c.header.Cells)
	}
	for _, row := range allRows {
		output = append(output, row.Cells)
	}
	return output
}

func (c *coloredTableWriter) SetAddRowFunc(f func(obj interface{}) Row) {
	c.addRowFunc = f
}

func (c *coloredTableWriter) AddHeader(names ...string) {
	cells := make([]Cell, 0)
	for _, name := range names {
		cells = append(cells, NewCell(name))
	}
	c.header = Row{Cells: cells}
}

func (c *coloredTableWriter) AddRow(obj interface{}) {
	c.rows = append(c.rows, c.addRowFunc(obj))
}

func (c *coloredTableWriter) Flush() {
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
				padAmount := sep[i] - len(col.Value) + 2
				_, _ = fmt.Fprint(c.writer, strings.Repeat(" ", padAmount))
			}
		}
	}
	return
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
			widths[i] = max(widths[i], len(col.Value))
		}
	}
	return widths
}
