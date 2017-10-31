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

package mixer

import (
	"fmt"
	"sync/atomic"

	"github.com/golang/protobuf/ptypes"
	google_rpc "github.com/googleapis/googleapis/google/rpc"
	"golang.org/x/net/context"

	"istio.io/pilot/test/mixer/istio_mixer_v1"
	words "istio.io/pilot/test/mixer/wordlist"
)

// Server is a basic Mixer server
type Server struct {
	words   []string
	counter uint64
}

// Check implements service check
func (s *Server) Check(ctx context.Context, msg *istio_mixer_v1.CheckRequest) (*istio_mixer_v1.CheckResponse, error) {
	atomic.AddUint64(&s.counter, 1)
	bag := s.decode(msg.Attributes)
	fmt.Printf("[%d] words.size=%d\n", s.counter, len(msg.Attributes.Words))
	for k, v := range bag {
		fmt.Printf("[%d] %s=%s\n", s.counter, k, v)
	}
	return &istio_mixer_v1.CheckResponse{
		Precondition: &istio_mixer_v1.CheckResponse_PreconditionResult{
			Status: &google_rpc.Status{Code: 0},
		},
	}, nil
}

// Report implements service report
func (s *Server) Report(ctx context.Context, _ *istio_mixer_v1.ReportRequest) (*istio_mixer_v1.ReportResponse, error) {
	// do nothing deliberately
	return &istio_mixer_v1.ReportResponse{}, nil
}

// NewServer creates a new server
func NewServer() *Server {
	out := &Server{
		words: words.GlobalList(),
	}
	fmt.Printf("GlobalList=%d\n", len(out.words))
	return out
}

func (s *Server) decode(attrs *istio_mixer_v1.CompressedAttributes) map[string]string {
	word := func(i int32) string {
		if i < 0 {
			// -1 translates to 0
			j := -i - 1
			if len(attrs.Words) > int(j) {
				return attrs.Words[j]
			}
			return fmt.Sprintf("#unknown:%d", i)
		}

		if len(s.words) > int(i) {
			return s.words[i]
		}
		return fmt.Sprintf("#unknown:%d", i)
	}

	out := make(map[string]string)

	for k, v := range attrs.Strings {
		out[word(k)] = word(v)
	}
	for k, v := range attrs.Int64S {
		out[word(k)] = fmt.Sprintf("%d", v)
	}
	for k, v := range attrs.Doubles {
		out[word(k)] = fmt.Sprintf("%f", v)
	}
	for k, v := range attrs.Bools {
		out[word(k)] = fmt.Sprintf("%t", v)
	}
	for k, v := range attrs.Timestamps {
		d, err := ptypes.Timestamp(v)
		if err != nil {
			out[word(k)] = err.Error()
		} else {
			out[word(k)] = d.String()
		}
	}
	for k, v := range attrs.Durations {
		d, err := ptypes.Duration(v)
		if err != nil {
			out[word(k)] = err.Error()
		} else {
			out[word(k)] = d.String()
		}
	}
	for k, v := range attrs.Bytes {
		out[word(k)] = fmt.Sprintf("%#v", v)
	}
	for k, m := range attrs.StringMaps {
		for v, w := range m.Entries {
			out[word(k)+"#"+word(v)] = word(w)
		}
	}

	return out
}
