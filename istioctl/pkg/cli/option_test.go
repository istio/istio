package cli

import (
	"testing"

	"github.com/spf13/pflag"
	"istio.io/istio/pkg/test/util/assert"
	"k8s.io/client-go/rest"
)

func Test_AddRootFlags(t *testing.T) {
	flags := &pflag.FlagSet{}
	r := AddRootFlags(flags)
	impersonateConfig := rest.ImpersonationConfig{}
	assert.Equal(t, *r.impersonate, impersonateConfig.UserName)
	assert.Equal(t, *r.impersonateUID, impersonateConfig.UID)
	assert.Equal(t, *r.impersonateGroup, impersonateConfig.Groups)
}
