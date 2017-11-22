# Google Deployment Manager Template

This directory contains a Google Cloud Deployment Manager template for getting
up-and-running with a Google Cloud Kubernetes Engine cluster with Istio
included.

If you have the Google Cloud SDK installed (get it [here](https://cloud.google.com/sdk/)), you can create a new deployment via the command:
```
$ gcloud deployment-manager deployments create my-istio-deployment --config=istio-cluster.yaml
```

See the file `istio-cluster.yaml` and `istio-cluster.schema` for details on customization.
