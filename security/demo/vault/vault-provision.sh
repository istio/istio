#!/bin/bash

# This script should be run by an admin to start Vault server,
# configure the authentication method of Vault, and provision
# the key and cert for siging CSR.

# Start Vault server
. ./vault-start-server.sh
vault status

# Get the k8s host address, which is used in "vault write auth/kubernetes/config kubernetes_host=..."
k8s_host_ip=$(kubectl get endpoints|grep kubernetes|tr -s ' '| cut -d ' ' -f 2 | cut -d ':' -f 1)
echo "k8s host IP is: ${k8s_host_ip}"

# Get the k8s api server certificate
k8s_default_token_name=$(kubectl get secrets|grep default-token|tr -s ' '| cut -d ' ' -f 1)
echo "k8s default token name is ${k8s_default_token_name}"
# Use the default token name to extract the k8s ca certificate,
# which is used in the kubernetes_ca_cert parameter for Vault.
kubectl get secrets ${k8s_default_token_name} -o jsonpath="{.data['ca\.crt']}" | base64 -d > $HOME/.kube/k8s_ca.crt
cat ~/.kube/k8s_ca.crt | openssl x509 -text

# Create a k8s service account for Vault to authenticate the k8s service account received
# with a CSR on k8s API server.
kubectl create -f ./yaml_config/vault-reviewer-sa.yaml
# Create the RBAC rule for authenticating k8s service accounts
kubectl apply -f ./yaml_config/vault-reviewer-rbac.yaml
# Create a k8s service account for Citadel to sign a CSR
kubectl create -f ./yaml_config/vault-citadel-sa.yaml
# Example Citadel SA JWT is in the form:
# {  
#   "alg":"RS256",
#   "typ":"JWT"
# }
# {  
#   "iss":"kubernetes/serviceaccount",
#   "kubernetes.io/serviceaccount/namespace":"default",
#   "kubernetes.io/serviceaccount/secret.name":"vault-citadel-sa-token-wthzt",
#   "kubernetes.io/serviceaccount/service-account.name":"vault-citadel-sa",
#   "kubernetes.io/serviceaccount/service-account.uid":"fae15195-5e32-11e8-aad2-42010a8a001d",
#   "sub":"system:serviceaccount:default:vault-citadel-sa"
# }

# Get the sa of vault-reviewer-sa
reviewer_sa=$(kubectl get secret $(kubectl get serviceaccount vault-reviewer-sa \
-o jsonpath={.secrets[0].name}) -o jsonpath={.data.token} | base64 -d -)
echo "The SA of vault-reviewer-sa is $reviewer_sa"
# Save the reviewer token for Citadel, which is used when authenticating a k8s SA at API server.
# When starting Citadel, reviewer-token.jwt file will be read by Citadel.
echo -n "$reviewer_sa" > reviewer-token.jwt

# Obsolete: log in as root to configure Vault. When prompted, enter the root token, e.g., "myroot" is a debug-only root token
# vault login

# Enable the Vault's k8s auth backend
vault auth enable kubernetes
# Configure the Vault's k8s auth backend
vault write auth/kubernetes/config \
  token_reviewer_jwt=$reviewer_sa  \
  kubernetes_host="https://$k8s_host_ip" \
  kubernetes_ca_cert=@"$HOME/.kube/k8s_ca.crt"

# Create a policy, called istio-cert, for vault to issue an istio cert.
vault policy write istio-cert vault_config/istio-cert.hcl
vault read sys/policy/istio-cert

# Create a role bound to the policy istio-cert under the k8s auth method.
# The token generated using this role is good for 36000 seconds.
vault write auth/kubernetes/role/istio-cert \
  bound_service_account_names=vault-citadel-sa \
  bound_service_account_namespaces=default \
  policies=istio-cert \
  period=36000s
vault read auth/kubernetes/role/istio-cert

# Create a PKI secret engine under the path istio_ca for issuing certificates
vault secrets enable -path=istio_ca -description="Istio CA" pki
# Set the global maximum TTL to be 1000 hour, which limits the expiration time of a certificate signed by this pki to be 1000 hour
vault secrets tune -max-lease-ttl=1000h istio_ca
# Set CA signing key in Vault
# Here istio_ca.pem is a file containing the private key and the certificate of the Istio CA.
vault write istio_ca/config/ca  pem_bundle=@./cert/istio_ca.pem
# Configuring the CA and CRL endpoints
vault write istio_ca/config/urls issuing_certificates="${VAULT_ADDR}/v1/istio_ca/ca" crl_distribution_points="${VAULT_ADDR}/v1/istio_ca/crl"
# Define a role in the Vault PKI backend to configure the certificate TTL, the key length, the SAN requirements, and other certificate attributes.
#vault write istio_ca/roles/istio-pki-role max_ttl=1h ttl=1h allow_any_name=true require_cn=false allowed_uri_sans="spiffe://example.com/ns/default/sa/example-pod-sa"
#vault write istio_ca/roles/istio-pki-role max_ttl=1h ttl=1h allow_any_name=true require_cn=false allowed_uri_sans="spiffe://*"
vault write istio_ca/roles/istio-pki-role max_ttl=1h ttl=1h allow_any_name=true require_cn=false allowed_uri_sans="*"
vault read istio_ca/roles/istio-pki-role
#curl     --header "X-Vault-Token: myroot"     --request POST     --data @./cert/csr_payload.json     $VAULT_ADDR/v1/istio_ca/sign/istio-pki-role

# After running ./vault-provision.sh:
# - update the "--vault-ip", "35.233.139.234" in go/src/istio.io/istio/security/docker/Dockerfile.citadel-vault-test-1. 
# - copy the updated reviewer-token.jwt to go/src/istio.io/istio/security/tests/integration/vaultTest/testdata/reviewer-token.jwt
# - rebuild and push the Dockerfile.citadel-vault-test-1

# Test signing a CSR
# vault write istio_ca/sign-verbatim name=workload_role format=pem ttl=1h csr=@./cert/workload-1.csr
# For debugging purpose, use "X-Vault-Token: myroot" to specify the root token
#curl \
#    --header "X-Vault-Token: 1036522c-d793-73eb-b5ae-821fcd9619c9" \
#    --request POST \
#    --data @./cert/csr_payload.json \
#    $VAULT_ADDR/v1/istio_ca/sign-verbatim

## The response:
#
#{
#    "request_id":"902ddfb4-338c-63c0-a9dc-efbd2798365f",
#    "lease_id":"",
#    "renewable":false,
#    "lease_duration":0,
#    "data":{
#        "ca_chain":["-----BEGIN CERTIFICATE-----\nMIIDzzCCAregAwIBAgIBAjANBgkqhkiG9w0BAQUFADB0MRMwEQYKCZImiZPyLGQB\nGRYDb3JnMRYwFAYKCZImiZPyLGQBGRYGc2ltcGxlMRMwEQYDVQQKDApTaW1wbGUg\nSW5jMRcwFQYDVQQLDA5TaW1wbGUgUm9vdCBDQTEXMBUGA1UEAwwOU2ltcGxlIFJv\nb3QgQ0EwHhcNMTcxMjE5MTgxNjQzWhcNMjcxMjE5MTgxNjQzWjB6MRMwEQYKCZIm\niZPyLGQBGRYDb3JnMRYwFAYKCZImiZPyLGQBGRYGc2ltcGxlMRMwEQYDVQQKDApT\naW1wbGUgSW5jMRowGAYDVQQLDBFTaW1wbGUgU2lnbmluZyBDQTEaMBgGA1UEAwwR\nU2ltcGxlIFNpZ25pbmcgQ0EwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIB\nAQDJ+JF26KezIqLph4njqTttxN1M0xr9dH9Iac8HsVw7qJel8lXl3bvoUkyE5tX5\n+60hdktE9zrGUYcHX+DxPYMBbODvY1d/IOLNbUn+C94LvhN7Orfacgb9XrW3ykh6\nDuk7ND0NJlHt7KAgU6Xpc3AfFBcTKysunLEApST11/q8IkD6lY5X5tnegthrNLzW\n1c3sJnvE5dH2M86c757+PpdyHuA5dCnL7jQ39QSp85UVtFQdORoco9zFlo3Wz4I6\nNS1CfT1v/goCJvMYZ2g/m9xjRIPw8sqo4pLZ/scIVkaeX3jpmbb5AmuzzBhhqyfx\nKPPCcLKcfAysSw8y6izJqm4ZAgMBAAGjZjBkMA4GA1UdDwEB/wQEAwIBBjASBgNV\nHRMBAf8ECDAGAQH/AgEAMB0GA1UdDgQWBBSgpCqOvc9s0Mxk8K8FOU8sKydB6TAf\nBgNVHSMEGDAWgBQmSoUIZttF2Xyk3ozcT268w0GicjANBgkqhkiG9w0BAQUFAAOC\nAQEAd7eE5UcHFPIpzm+XIHFz4QrytBpfUSMi9DrRTqHLnBZLS9Af1L4rrV5OgCMf\ndt8iUcHps+3tj9+qEVSNBfc8W0T5V6BnNcE5eqvCdMEdW+TpCeEAuYc4eE/ikTxv\nSwapAz3Ta9ht1mzE7F14hPAe/H83Il8Q2mZerHRWKfpQo6GxqhZLsB5su6gdmv9u\nw+LvuNJefvYcCW2DnJC8DSCVAXXqlby2/MK6odYHhZyJ5m2GJuTYAE79G5RDJmlo\n43opDs1UqsfCcIZOP0dtkQc29TWb755YzUg/8x/8sdTWA4WJQ+XlAvcEsrVGcJFv\n3kFyk6jXoxpR66M9xEwDJ7Bk1Q==\n-----END CERTIFICATE-----","-----BEGIN CERTIFICATE-----\nMIIDxjCCAq6gAwIBAgIBATANBgkqhkiG9w0BAQUFADB0MRMwEQYKCZImiZPyLGQB\nGRYDb3JnMRYwFAYKCZImiZPyLGQBGRYGc2ltcGxlMRMwEQYDVQQKDApTaW1wbGUg\nSW5jMRcwFQYDVQQLDA5TaW1wbGUgUm9vdCBDQTEXMBUGA1UEAwwOU2ltcGxlIFJv\nb3QgQ0EwHhcNMTcxMjE5MTgxMzI5WhcNMjcxMjE5MTgxMzI5WjB0MRMwEQYKCZIm\niZPyLGQBGRYDb3JnMRYwFAYKCZImiZPyLGQBGRYGc2ltcGxlMRMwEQYDVQQKDApT\naW1wbGUgSW5jMRcwFQYDVQQLDA5TaW1wbGUgUm9vdCBDQTEXMBUGA1UEAwwOU2lt\ncGxlIFJvb3QgQ0EwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQCo4/Az\nHwADHUNb+jbFx1PT10YRdAXnmc7RKdnTG27I1qHOC85g+1irxu8Bl4BWsBIbL7kM\nfM/g/HILEAvQGRRmtGGt9kvq7KogsuzOlBQWFDv+5O6yq2Zn48NzvbKY81VUz1fZ\nWUTULoRTbzgitN5vhEElCS9co63cj4XQ3+yIKQKj28Wr/5ZZtnM4cn8F1wAb/JEb\nt2A93AG1ToZ3JDzQlIv5HWm1uPs3KbSKd5N13p9YuKwQFqZ8uyM5IugYGhilWged\nNt8EMI0rVblIKogSbuQg5JY20QqbXr/sMSEico0tNI/U9RLzAffPS3RMOVzLpIBU\n1C00o/5vGrXMHOfDAgMBAAGjYzBhMA4GA1UdDwEB/wQEAwIBBjAPBgNVHRMBAf8E\nBTADAQH/MB0GA1UdDgQWBBQmSoUIZttF2Xyk3ozcT268w0GicjAfBgNVHSMEGDAW\ngBQmSoUIZttF2Xyk3ozcT268w0GicjANBgkqhkiG9w0BAQUFAAOCAQEABJBS1zlZ\nOmlEBw8MtlGQ+OQFVu8gadpEl4ogqRCAYrDW5BD/8PQGCaLIlsElVkJNmIJJSVAs\n0oM9UzcpClisuEOxFmcpux1li9661HKek7GlEEYcqbymOWc4f17pa7uGhfCvJ671\nJe6dvHOWZIACnOq12kHgt7G37GiAkJek9Onz2qYUu01zM2AJ2u45uhEHiXCyx/xJ\naTUHtkq6dLOHFo7YAkViu9TfVxWCQDlCptqmpVZM5nzuqiidx3H9D7pKyrJI1f+x\n+4Ufyc1yMFNIaZKUFuDjUtCPSUd4N6trFy6BCYaPyLO+nqRILunUaMKSw25mYITk\nNO+EkKXA8BeEKQ==\n-----END CERTIFICATE-----"],
#        "certificate":"-----BEGIN CERTIFICATE-----\nMIIEHTCCAwWgAwIBAgIUYBp2BcIrOq5Xpakb3bK9mmH8wsQwDQYJKoZIhvcNAQEL\nBQAwejETMBEGCgmSJomT8ixkARkWA29yZzEWMBQGCgmSJomT8ixkARkWBnNpbXBs\nZTETMBEGA1UECgwKU2ltcGxlIEluYzEaMBgGA1UECwwRU2ltcGxlIFNpZ25pbmcg\nQ0ExGjAYBgNVBAMMEVNpbXBsZSBTaWduaW5nIENBMB4XDTE4MDUyMzAzMDA0OVoX\nDTE4MDUyMzA0MDExOVowEzERMA8GA1UEAxMId29ya2xvYWQwggEiMA0GCSqGSIb3\nDQEBAQUAA4IBDwAwggEKAoIBAQCtUKHNG598mQ0wo5+AfZhn2yA8HhL1QV0XERJg\nBU2pPbH/4yIHq++kugWWbj4REE7OPvKjJRdo8yJ9OpjDXA8s5t7fchdr6BePLF6+\nGfkQACmnKAziRHMg22Zy+crdVEiyrMAzwujbiBxiI5hcHHB15TX+6lAxaLZJ3BLC\n4NBdYHUeEwvuBV4zLLvKSVE6jFQIvxHKk/Nh/sJvvvSIOWmXPgS6raFPKPTDJ3Mj\nFyCUVEz8/HWyaEptX4C91NQxa7/CIJ/DYXtKVbP+jXGaLrLQUX+2r95H2cU604Of\nMz2ZPmYgYUovtb93llwgLKoJk3MjIGEvy4AluGqegrDe5ghfAgMBAAGjggEAMIH9\nMB0GA1UdDgQWBBTx0f33Ijtu8t8tL2+KhZA4GPEW7TAfBgNVHSMEGDAWgBSgpCqO\nvc9s0Mxk8K8FOU8sKydB6TBFBggrBgEFBQcBAQQ5MDcwNQYIKwYBBQUHMAKGKWh0\ndHA6Ly8zNS4yMzMuMTc1LjIxNzo4MjAwL3YxL2lzdGlvX2NhL2NhMDsGA1UdHwQ0\nMDIwMKAuoCyGKmh0dHA6Ly8zNS4yMzMuMTc1LjIxNzo4MjAwL3YxL2lzdGlvX2Nh\nL2NybDA3BgNVHREEMDAuhixzcGlmZmU6Ly9jbHVzdGVyLmxvY2FsL25zL2RlZmF1\nbHQvc2EvZGVmYXVsdDANBgkqhkiG9w0BAQsFAAOCAQEAieJ7HkfjSzzQiutrtU93\nyn
#maqJgiPgLqlAoncNJiE5tLbIteHESTS/Je1Vsa7J6gGdEbCUL7F7msttgC5KE6\nFReTBM2yfni8+3gjgQOzLtDMcp98z+9OJZaQ3Z3/LeOo3d3cFRbO8vc6cRkxM/sC\nSQl3HWX3AudkLtU3/7UrEsYzRE/LIXfQCDdfBO+c1CncoBKWcxABZrGxbXREMy2i\ncWdvRhfSLGC7g9j8xOjoYQTqoba8orOhCknVSuK+rEGlXL6OR00b8xu0IDW1+IVL\nnNSeY4ujmXHWj85RErS19XxwukRp4tErcQ0ujf7REZUimOdi8RvGzB8nsTNJOB4A\ndw==\n-----END CERTIFICATE-----",
#        "issuing_ca":"-----BEGIN CERTIFICATE-----\nMIIDzzCCAregAwIBAgIBAjANBgkqhkiG9w0BAQUFADB0MRMwEQYKCZImiZPyLGQB\nGRYDb3JnMRYwFAYKCZImiZPyLGQBGRYGc2ltcGxlMRMwEQYDVQQKDApTaW1wbGUg\nSW5jMRcwFQYDVQQLDA5TaW1wbGUgUm9vdCBDQTEXMBUGA1UEAwwOU2ltcGxlIFJv\nb3QgQ0EwHhcNMTcxMjE5MTgxNjQzWhcNMjcxMjE5MTgxNjQzWjB6MRMwEQYKCZIm\niZPyLGQBGRYDb3JnMRYwFAYKCZImiZPyLGQBGRYGc2ltcGxlMRMwEQYDVQQKDApT\naW1wbGUgSW5jMRowGAYDVQQLDBFTaW1wbGUgU2lnbmluZyBDQTEaMBgGA1UEAwwR\nU2ltcGxlIFNpZ25pbmcgQ0EwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIB\nAQDJ+JF26KezIqLph4njqTttxN1M0xr9dH9Iac8HsVw7qJel8lXl3bvoUkyE5tX5\n+60hdktE9zrGUYcHX+DxPYMBbODvY1d/IOLNbUn+C94LvhN7Orfacgb9XrW3ykh6\nDuk7ND0NJlHt7KAgU6Xpc3AfFBcTKysunLEApST11/q8IkD6lY5X5tnegthrNLzW\n1c3sJnvE5dH2M86c757+PpdyHuA5dCnL7jQ39QSp85UVtFQdORoco9zFlo3Wz4I6\nNS1CfT1v/goCJvMYZ2g/m9xjRIPw8sqo4pLZ/scIVkaeX3jpmbb5AmuzzBhhqyfx\nKPPCcLKcfAysSw8y6izJqm4ZAgMBAAGjZjBkMA4GA1UdDwEB/wQEAwIBBjASBgNV\nHRMBAf8ECDAGAQH/AgEAMB0GA1UdDgQWBBSgpCqOvc9s0Mxk8K8FOU8sKydB6TAf\nBgNVHSMEGDAWgBQmSoUIZttF2Xyk3ozcT268w0GicjANBgkqhkiG9w0BAQUFAAOC\nAQEAd7eE5UcHFPIpzm+XIHFz4QrytBpfUSMi9DrRTqHLnBZLS9Af1L4rrV5OgCMf\ndt8iUcHps+3tj9+qEVSNBfc8W0T5V6BnNcE5eqvCdMEdW+TpCeEAuYc4eE/ikTxv\nSwapAz3Ta9ht1mzE7F14hPAe/H83Il8Q2mZerHRWKfpQo6GxqhZLsB5su6gdmv9u\nw+LvuNJefvYcCW2DnJC8DSCVAXXqlby2/MK6odYHhZyJ5m2GJuTYAE79G5RDJmlo\n43opDs1UqsfCcIZOP0dtkQc29TWb755YzUg/8x/8sdTWA4WJQ+XlAvcEsrVGcJFv\n3kFyk6jXoxpR66M9xEwDJ7Bk1Q==\n-----END CERTIFICATE-----",
#        "serial_number":"60:1a:76:05:c2:2b:3a:ae:57:a5:a9:1b:dd:b2:bd:9a:61:fc:c2:c4"
#    },
#    "wrap_info":null,
#    "warnings":null,
#    "auth":null
#}
#
#
#
