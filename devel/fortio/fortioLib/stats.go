// Copyright 2017 Istio Authors
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

package fortio

import (
	"fmt"
	"io"
	"log"
	"math"
)

// Counter is class to record values and calculate stats (count,average,min,max,stddev)
type Counter struct {
	Count        int64
	Min          float64
	Max          float64
	Sum          float64
	SumOfSquares float64
}

// Record records a data point
func (c *Counter) Record(v float64) {
	c.Count++
	if c.Count == 1 {
		c.Min = v
		c.Max = v
	} else if v < c.Min {
		c.Min = v
	} else if v > c.Max {
		c.Max = v
	}
	c.Sum += v
	c.SumOfSquares += (v * v)
}

// Avg returns the average.
func (c *Counter) Avg() float64 {
	return c.Sum / float64(c.Count)
}

// StdDev returns the standard deviation.
func (c *Counter) StdDev() float64 {
	fC := float64(c.Count)
	sigma := (c.SumOfSquares - c.Sum*c.Sum/fC) / fC
	return math.Sqrt(sigma)
}

// Printf prints stats
func (c *Counter) Printf(out io.Writer, msg string) {
	fmt.Fprintf(out, "%s : count %d avg %g +/- %.4g min %g max %g sum %g\n", msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum) // nolint(errorcheck)
}

// Log outputs the stats to the logger
func (c *Counter) Log(msg string) {
	log.Printf("%s : count %d avg %g +/- %.4g min %g max %g sum %g", msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum) // nolint(errorcheck)
}
