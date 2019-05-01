The integration tests in this directory use two test Vault servers hosted on Google Kubernetes Engine (GKE);
one is a TLS Vault server and the other is a non-TLS Vault server.

To set up a Vault server for testing the integration with a Citadel Agent, you can follow
the following steps.

Install and configure a Vault server. On Kubernetes platform, a detailed guide can be found
[here](https://evalle.xyz/posts/integration-kubernetes-with-vault-auth/), which includes:

- Generate X509 certificates using openssl commands for the Vault server.
Since the server is only for testing purpose, the expiration time of X509 certificates
can be set as a large number.

- Deploy the Vault server. On Kubernetes platform, the Vault server can be deployed
through a [Kubernetes deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#creating-a-deployment)
that ensures that there will be one replica of the Vault server.

- Initialize the Vault server and configure the [Kubernetes Auth Method](https://www.vaultproject.io/docs/auth/kubernetes.html)
for the Vault server.

After a Vault server is deployed and configured, you may expose the Vault server. 

- If the server is running on Kubernetes,
it can be exposed through a [LoadBalancer kubernetes service](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#configuration-file).
After obtaining the external IP fo the load balancer, you can reserve the load balancer IP as
a static IP (as opposed to an ephemeral IP addresses) so it will be a stable endpoint for the Vault service.
On GCP platform, an external IP can be reserved as a static IP through the dashboard on "VPC Network" for "External IP addresses".
If you have a DNS service provider, you may also set up a DNS name for the Vault endpoint.


The maintenance for the Vault server:

- On Kubernetes platform, through Kubernetes replica controller automatically monitoring
the state of the Vault service replica (including restarting the Vault pod), the maintenance should be minimum.
