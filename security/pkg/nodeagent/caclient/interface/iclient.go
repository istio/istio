// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package iclient

import (
	"context"
)

// Client interface defines the clients need to implement to talk to CA for CSR.
type Client interface {
	CSRSign(ctx context.Context, csrPEM []byte, subjectID string,
		certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error)
}
