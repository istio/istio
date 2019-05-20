# Running E2E tests on your own kubernetes cluster

* [Step 1: Setup GCP](#step-1-setup-gcp)
* [Step 2: Setup a Cluster](#step-2-setup-a-cluster)
* [Step 3: Setup Istio Environment Variables](#step-3-setup-istio-environment-variables)

## Step 1: Setup GCP
This section walks you through the one-time setup for creating and configuring a Google Coud Platform (GCP) project.

#### Install Google Cloud SDK
If you haven't already installed the Google Cloud SDK installed, follow the instructions [here](https://cloud.google.com/sdk/).

If you're not sure if you have it installed, you can check with:

```bash
which gcloud
```

#### Create a Project

If you haven't already, [create a Google Cloud Project](https://cloud.google.com/resource-manager/docs/creating-managing-projects).
    
#### Configure GCE/GKE Service Account
You must grant your default compute service account the correct permissions before creating the deployment.
Otherwise, the installation will fail.

> Note the **Project Number** for your project by visiting the [Dashboard Page](https://console.cloud.google.com/homehttps://console.cloud.google.com/home) of Google Cloud Console.

Nativate to the [IAM
section](https://console.cloud.google.com/permissions/projectpermissions)
of the Google Cloud Console and make sure that your
default compute service account (by default
`[PROJECT_NUMBER]-compute@developer.gserviceaccount.com`)
includes the following roles:

* **Kubernetes Engine Admin** (`roles/container.admin`)
* **Editor** (`roles/editor`, included by default)

## Step 2: Setup a Cluster

The following steps are automated with the script `create_cluster_gke.sh` located in this directory. To create a cluster with the script, simply run:

```bash
./tests/integration/create_cluster_gke.sh -c ${CLUSTER_NAME}
```

To list the options for the script, you can get help via `-h`:

```bash
./tests/integration/create_cluster_gke.sh -h
```

#### Create the Cluster

E2E tests require a Kubernetes cluster. You can create one using the Google Container Engine using
the following command:

```bash
gcloud container clusters \
  create ${CLUSTER_NAME} \
  --zone ${ZONE} \
  --project ${PROJECT_NAME} \
  --cluster-version ${CLUSTER_VERSION} \
  --machine-type ${MACHINE_TYPE} \
  --num-nodes ${NUM_NODES} \
  --enable-kubernetes-alpha \
  --no-enable-legacy-authorization
 ```

 - `CLUSTER_NAME`: Whatever suits your fancy, 'istio-e2e' is a good choice.
 - `ZONE`: 'us-central1-f' is a good value to use.
 - `PROJECT_NAME`: is the name of the GCP project that will house the cluster. You get a project by visiting [GCP](https://console.cloud.google.com).
 - `CLUSTER_VERSION`: 1.7.3 or later.
 - `MACHINE_TYPE`: Use 'n1-standard-4'
 - `NUM_NODES`: Use 3.
 - `no-enable-legacy-authorization`: Optional, needed if you want to test RBAC.

#### Get cluster credentials
```
gcloud container clusters get-credentials ${CLUSTER_NAME} --zone ${ZONE} --project ${PROJECT_NAME}
```

#### Grant Admin Permission
```
kubectl create clusterrolebinding myname-cluster-admin-binding  --clusterrole=cluster-admin  --user=$(gcloud config get-value core/account)
```

## Step 3: Setup Istio Environment Variables

#### Option 1: Build your own images

You can set the **HUB** and **TAG** environment variables to point to your own Docker registry.
Additionally, you can also set **GS_BUCKET** to use a different Google Storage Bucket than the default one 
(you need write permissions) it allows you to customize Makefile rules.

For example:

```bash
export HUB=myname
export TAG=latest
export GS_BUCKET=mybucket
```

Then you can build and push the docker images to your registry:
```bash
# Build images on the local docker.
make docker

# Push images to docker registry
make push
```

On MacOS, you need to set the target operating system before building the images
```
GOOS=linux make docker push
```

#### Option 2: Use pre-built Istio images
In this case, you'll need to specify the image SHA in the TAG variable. You can pick any SHA available on istio/istio.

```bash
export HUB="gcr.io/istio-testing"
export TAG="d0142e1afe41c18917018e2fa85ab37254f7e0ca"
```
