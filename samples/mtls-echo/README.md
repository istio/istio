# mtls-echo

This sample demonstrates Istio's mtls origination functionality. The echo server requires and verifies the client certificate for all connections to an mtls port. When a validated client certificate is present, the http server adds `ClientCertSubject` and `ClientCertSerialNumber` into the standard HTTP echo response.
