# Setup Local Registry in IBM Bluemix Container Service(Armada)

## Step 1:
Setup a Kubernetes cluster on Bluemix

## Step 2:
Enable insecure registry on all nodes of the cluster

Deploy registry into the cluster
```bash
kubectl apply -f $ISTIO/tests/util/localregistry/localregistry.yaml
```

Get the registry service cluster ip
```bash
kubectl get service kube-registry -n kube-system -o jsonpath='{.spec.clusterIP}'
```

Get all nodes' ip by 
```bash
kubectl get nodes
```

Use [runon](https://github.ibm.com/kubernetes-tools/runon/blob/master/runon) to get a bash into the node:
```bash
./runon <node ip> bash
```

Edit the docker service
```bash
systemctl edit docker
```
And copy the following content into the file and save the file:
```
[Service]
ExecStart=
ExecStart=/usr/bin/dockerd -H fd:// --insecure-registry <registry service ip>:5000
```

Reload the daemon and restart the docker service
```bash
systemctl daemon-reload
systemctl restart docker
```

Now you should exit the node. Restart all the nodes by 
```bash
bx cs reboot-workers <cluster name> <node name>
```

After all nodes are ready, you can check the status of docker service in each node:
```bash
./runon <node ip> bash
systemctl status docker
```

You should see `active (running)` in `Active` field and `/usr/bin/dockerd -H fd:// --insecure-registry <registry service ip>:5000` in the `CGroup` field.
