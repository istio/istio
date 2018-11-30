#!/bin/bash

# This script should be run by an admin to start Vault server,
# configure the authentication method of Vault, and provision
# the key and cert for siging CSR.

# Start Vault server
source ./vault-start-server.sh

# Get the k8s host address, which is used in "vault write auth/kubernetes/config kubernetes_host=..."
k8s_host_ip=$(kubectl get endpoints|grep kubernetes|tr -s ' '| cut -d ' ' -f 2 | cut -d ':' -f 1)
echo "k8s host IP is: ${k8s_host_ip}"

# Get the k8s api server certificate
k8s_default_token_name=$(kubectl get secrets|grep default-token|tr -s ' '| cut -d ' ' -f 1)
echo "k8s default token name is ${k8s_default_token_name}"
# Use the default token name to extract the k8s ca certificate,
# which is used in the kubernetes_ca_cert parameter for Vault.
kubectl get secrets ${k8s_default_token_name} -o jsonpath="{.data['ca\.crt']}" | base64 --decode > $HOME/.kube/k8s_ca.crt

# Create a k8s service account for Vault to authenticate the k8s service account received
# with a CSR on k8s API server.
kubectl create -f ../yaml_config/vault-reviewer-sa.yaml
# Create the RBAC rule for authenticating k8s service accounts
kubectl apply -f ../yaml_config/vault-reviewer-rbac.yaml
# Create a k8s service account for Citadel to sign a CSR
kubectl create -f ../yaml_config/vault-citadel-sa.yaml
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
-o jsonpath={.secrets[0].name}) -o jsonpath={.data.token} | base64 --decode -)
echo "The SA of vault-reviewer-sa is $reviewer_sa"
# Save the reviewer token for Citadel, which is used when authenticating a k8s SA at API server.
# When starting Citadel, reviewer-token.jwt file will be read by Citadel.
echo -n "$reviewer_sa" > reviewer-token.jwt

vault login myroot
# Enable the Vault's k8s auth backend
vault auth enable kubernetes
# Configure the Vault's k8s auth backend
vault write auth/kubernetes/config \
  token_reviewer_jwt=$reviewer_sa  \
  kubernetes_host="https://$k8s_host_ip" \
  kubernetes_ca_cert=@"$HOME/.kube/k8s_ca.crt"

# Create a policy, called istio-cert, for vault to issue an istio cert.
vault policy write istio-cert ../vault_config/istio-cert.hcl
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
vault write istio_ca/config/ca  pem_bundle=@../cert/istio_ca.pem
# Configuring the CA and CRL endpoints
vault write istio_ca/config/urls issuing_certificates="${VAULT_ADDR}/v1/istio_ca/ca" crl_distribution_points="${VAULT_ADDR}/v1/istio_ca/crl"
# Define a role in the Vault PKI backend to configure the certificate TTL, the key length, the SAN requirements, and other certificate attributes.
vault write istio_ca/roles/istio-pki-role max_ttl=1h ttl=1h allow_any_name=true require_cn=false allowed_uri_sans="*"
vault read istio_ca/roles/istio-pki-role


