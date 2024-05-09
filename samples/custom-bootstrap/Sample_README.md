1. Deploy istio sample app Bookinfo per https://istio.io/latest/docs/examples/bookinfo/
2. Extract basic-auth.wasm from ghcr.io/istio-ecosystem/wasm-extensions/basic_auth:1.12.0
3. Create configmap for istio custom bootstrap
4. Edit deployment for product page service with annotations to mount into istio-proxy container a persistent volume where the basic-auth.wasm is store
5. port-forward port 1080 to local port e.g. 1080
6. curl -v localhost:1080/productpage, should receive 401 unauthorized
7. curl -v -u ok:test localhost:1080/productpage, should return normal product page
