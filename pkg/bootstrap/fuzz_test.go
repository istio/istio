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

package bootstrap

import (
	"bytes"
	"io"
	"os"
	"testing"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

func FuzzWriteTo(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		ff := fuzz.NewConsumer(data)

		// create config
		cfg := Config{}

		err := ff.GenerateStruct(&cfg)
		if err != nil {
			return
		}

		if cfg.Metadata == nil {
			return
		}
		if cfg.Metadata.ProxyConfig == nil {
			return
		}

		i := New(cfg)

		// create template file
		templateBytes, err := ff.GetBytes()
		if err != nil {
			return
		}

		tf, err := os.Create("templateFile")
		if err != nil {
			return
		}
		defer func() {
			tf.Close()
			os.Remove("templateFile")
		}()
		_, err = tf.Write(templateBytes)
		if err != nil {
			return
		}

		// call target
		var buf bytes.Buffer
		i.WriteTo("templateFile", io.Writer(&buf))
	})
}
