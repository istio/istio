package wasm

import (
	"encoding/base64"
	"fmt"

	"istio.io/istio/pkg/test/framework/components/registryredirector"
	"istio.io/istio/pkg/test/framework/resource"
	common "istio.io/istio/tests/integration/telemetry/stats/prometheus"
)

var (
	registry registryredirector.Instance
)

const (
	// Same user name and password as specified at pkg/test/fakes/imageregistry
	registryUser   = "user"
	registryPasswd = "passwd"
)

func registrySetup(ctx resource.Context) (err error) {
	if common.GetAppNamespace() == nil {
		common.AppNameSpaceSetup(ctx)
	}

	registry, err = registryredirector.New(ctx, registryredirector.Config{
		Cluster: ctx.AllClusters().Default(),
	})
	if err != nil {
		return
	}

	args := map[string]interface{}{
		"DockerConfigJson": base64.StdEncoding.EncodeToString(
			[]byte(createDockerCredential(registryUser, registryPasswd, registry.Address()))),
	}
	if err := ctx.ConfigIstio().EvalFile(common.GetAppNamespace().Name(), args, "testdata/registry-secret.yaml").
		Apply(); err != nil {
		return err
	}
	return nil
}

func createDockerCredential(user, passwd, registry string) string {
	credentials := `{
	"auths":{
		"%v":{
			"username": "%v",
			"password": "%v",
			"email": "test@abc.com",
			"auth": "%v"
		}
	}
}`
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + passwd))
	return fmt.Sprintf(credentials, registry, user, passwd, auth)
}
