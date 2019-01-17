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

package env

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestEnvDur(t *testing.T) {
	cases := []struct {
		name   string
		envVal string
		def    time.Duration
		want   time.Duration
	}{
		{"TestEnvDur", "", time.Second, time.Second},
		{"TestEnvDur", "2s", time.Second, 2 * time.Second},
		{"TestEnvDur", "2blurbs", time.Second, time.Second},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] name=%v def=%v", i, c.name, c.def), func(tt *testing.T) {

			if c.envVal != "" {
				prev := os.Getenv(c.name)
				if err := os.Setenv(c.name, c.envVal); err != nil {
					tt.Fatal(err)
				}
				defer os.Setenv(c.name, prev)
			}
			if got := Duration(c.name, c.def); got != c.want {
				tt.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestEnvInt(t *testing.T) {
	cases := []struct {
		name   string
		envVal string
		def    int
		want   int
	}{
		{"TestEnvInt", "", 3, 3},
		{"TestEnvInt", "6", 5, 6},
		{"TestEnvInt", "six", 5, 5},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] name=%v def=%v", i, c.name, c.def), func(tt *testing.T) {

			if c.envVal != "" {
				prev := os.Getenv(c.name)
				if err := os.Setenv(c.name, c.envVal); err != nil {
					tt.Fatal(err)
				}
				defer os.Setenv(c.name, prev)
			}
			if got := Integer(c.name, c.def); got != c.want {
				tt.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
