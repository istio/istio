# Sample JWT and JWKS data for demo

This folder contains sample data to setup end-user authentication with Istio authentication policy, together with the script to (re)generate them.

## Example end-user authentication policy using the mock jwks.json data

```yaml
apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "jwt-example"
spec:
  targets:
  - name: httpbin
  origins:
  - jwt:
      issuer: "testing@secure.istio.io"
      jwksUri: "https://raw.githubusercontent.com/istio/istio/master/security/tools/jwt/samples/jwks.json"
  principalBinding: USE_ORIGIN
```

The `demo.jwt` contains a signed-JWT token with following payload:

```json
{
  "exp": 4685989700,
  "foo": "bar",
  "iat": 1532389700,
  "iss": "testing@secure.istio.io",
  "sub": "testing@secure.istio.io"
}
```

Note the expiration date (`exp`) is very long in the future, so it can be tested as is without anty modification. For example:

```bash
TOKEN=$(curl https://raw.githubusercontent.com/istio/istio/master/security/tools/jwt/samples/demo.jwt -s)
curl --header "Authorization: Bearer $TOKEN" $INGRESS_HOST/headers -s -o /dev/null -w "%{http_code}\n"
```

Alternatively, you can use the `gen-jwt.py` script to create new test token:

```
TOKEN=$(./gen-jwt.py key.pem --expire=300 --iss "new-issuer@secure.istio.io")
```

> Before you start, run the following command to install python dependences.

    ```
    pip install jwcrypto
    ```

## Regenerate private key and JWKS (for developer use only)

1. Regenerate private key using `openssl`

```
openssl genrsa -out key.pem 2048
```

2. Run gen-jwt.py with `--jkws` to create new public key set and demo JWT

```
gen-jwt.py key.pem -jwks=./jwks.json --expire=3153600000 --claims=foo:bar > demo.jwt
```