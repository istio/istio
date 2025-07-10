package k8sgateway

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

// VerifyMutatingWebhookConfigurations verifies that the proper number of mutating webhooks are running, used with
// revisions and revision tags, and retries until success or timeout.
func VerifyGatewaysProgrammed(ctx framework.TestContext, cs cluster.Cluster, names []types.NamespacedName) {
	scopes.Framework.Infof("=== verifying gateways are programmed === ")
	check := func() error {
		if err := CheckGatewaysProgrammed(ctx, cs, names); err != nil {
			return err
		}
		return nil
	}
	retry.UntilSuccessOrFail(ctx, check)
	scopes.Framework.Infof("=== succeeded ===")
}

// CheckGatewaysProgrammed checks that the gateways are programmed by checking the
// GatewayConditionProgrammed condition on the Gateway resource.
// Returns an error if any of the gateways are not programmed.
func CheckGatewaysProgrammed(ctx framework.TestContext, cs cluster.Cluster, names []types.NamespacedName) error {
	for _, nsName := range names {
		gw, err := cs.GatewayAPI().GatewayV1().Gateways(nsName.Namespace).Get(context.TODO(), nsName.Name, metav1.GetOptions{})
		if gw == nil {
			return fmt.Errorf("failed to find gateway: %s/%s: %v", nsName.Namespace, nsName.Name, err)
		}
		cond := kstatus.GetCondition(gw.Status.Conditions, string(k8sv1.GatewayConditionProgrammed))
		if cond == kstatus.EmptyCondition {
			return fmt.Errorf("failed to find programmed condition for gateway %s/%s: %+v", nsName.Namespace, nsName.Name, gw.Status.Conditions)
		}
		if cond.Status != metav1.ConditionTrue {
			return fmt.Errorf("gateway not programmed for gateway %s/%s: %+v", nsName.Namespace, nsName.Name, cond)
		}
		if cond.ObservedGeneration != gw.Generation {
			return fmt.Errorf("stale GWC generation for gateway %s/%s: %+v", nsName.Namespace, nsName.Name, cond)
		}
	}
	return nil
}
