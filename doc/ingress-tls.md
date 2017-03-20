# Ingress TLS termination
Istio ingress currently supports the capability to add ingress-wide TLS termination. To enable this option, create a Kubernetes secret containing a key and certificate and configure the ingress controller to use the secret to terminate TLS. When this option is enabled Istio ingress serves on port 443.
 
In the future, more complex setups will be supported.

## Generating keys
If necessary, a private key and certificate can be created for testing using [OpenSSL](https://www.openssl.org/).
```
openssl req -newkey rsa:2048 -nodes -keyout cert.key -x509 -days -out='cert.crt' -subj '/C=US/ST=Seattle/O=Example/CN=secure.example.io'
```

## Create the secret
Create the secret using `kubectl`.
```bash
kubectl -n <namespace> create secret generic <secret name> --from-file=tls.key=cert.key --from-file=tls.crt=cert.crt
```

## Configure Ingress controller
Inside the Ingress deployment, add `--namespace <namespace> --secret <secret name>` to the Istio Ingress controller arguments and change the service definition for the Istio Ingress controller to expose port 443 instead of port 80.

Deploy/redeploy the Ingress controller.