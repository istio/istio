# CSR Controller

This folder implements the CSR controller for Istio to sign Kubernetes CSR, please refer to [Kubernetes CSR API](https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/) for more detail. BTW, this controler reuse many code from [signer-ca](https://github.com/cert-manager/signer-ca).

## How to build image for CSR controller?

CSR controller can be built into binary file named `csrctrl`, then built the CSR controller image based on this binary file as below:

Generate the CSR controller binary file:

```console
$ make build csrctrl
```

Check the binary file under the folder of `istio/out/linux_amd64/`, then build the image based on it:

```console
$ make docker.csrctrl
```

User can find the CSR controller image by `docker images`, and see the $HUB/csrctrl with tag value $TAG.

## How to deploy CSR controller in Kubernetes cluster in test code?

If user want to deploy this CSR controller to do some test, there should be some template codes in user's test case:

```go
......
import "istio.io/istio/pkg/test/framework/components/csrctrl"
......

func TestCSRSigningViaCSRController(t *testing.T) {
  framework.
    NewTest(t).
    RequiresSingleCluster().
    Run(func(t framework.TestContext) {
      g := NewWithT(t)
      mycsrctrl := csrctrl.NewOrFail(t, t, csrctrl.Config{
        SignerNames:    "istio.io/foo,istio.io/bar",
        SignerRootPath: "/tmp/pki/signer",
      })
      // User's test code
      ......
  })
}
```

## Verify the CSR controller

Check the CSR controller pod under the namespace of `sig-ca-system` via below command:

```console
$ kubectl get pods -n sig-ca-system
```

The output looks like this:

```console
NAME                                      READY   STATUS    RESTARTS   AGE
signer-ca-controller-manager-xxxxxx-xxx   1/1     Running   0          21s
```
