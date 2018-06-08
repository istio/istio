# Setup Local Registry in IBM Bluemix Container Service

Get the registry service cluster ip
```shell
kubectl get service kube-registry -n kube-system -o jsonpath='{.spec.clusterIP}'
```

Get all nodes' name by 
```bash
kubectl get nodes
```

Use runon(thanks to [Nick Stott](github.com/nstott) built this script) to get a bash shell into the node:
```bash
./tools/runon <node name> bash
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
Bluemix:
```bash
bx cs reboot-workers <cluster name> <node name>
```

After all nodes are ready, you can check the status of docker service in each node:
```bash
./runon <node ip> bash
systemctl status docker
```

You should see `active (running)` in `Active` field and `/usr/bin/dockerd -H fd:// --insecure-registry <registry service ip>:5000` in the `CGroup` field.
