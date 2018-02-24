To run e2e test:

```bash
$ pwd
.../src/istio.io/istio
```

Export KUBECONFIG to your kube config file, e.g.,
```bash
$ export KUBECONFIG="[YourPath]/.kube/config"
```

Run e2e_piot
```bash
$ make e2e_pilot 
```

Replace e2e_pilot with other targets accordingly if necessary. The detail can
be found in tests/istio.mk.
