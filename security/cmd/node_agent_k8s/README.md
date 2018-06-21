*This page is deprecating. The new approach is under development.*

To play with this demo, you need to:

- Create a GKE cluster with version 1.8.7.

- Deploy the node agent as DaemonSet by

```shell
kubectl apply -f nodeagent.yaml
```

- Make sure the node agent is up and running
```shell
kubectl get pods
```

```shell
# Sample output
NAME                              READY     STATUS        RESTARTS   AGE
nodeagent-fs8lt                   1/1       Running       0          3m
nodeagent-qt6z6                   1/1       Running       0          3m
```

- Deploy the workload pod.
```shell
kubectl apply -f workload_app/workload.yaml
```

- Make sure the workload is up and running, and then check the log of node agent.
```shell
kubectl logs nodeagent-fs8lt
```

```shell
#Sample output
Node agent with both mgmt and workload api interfaces.

2018/02/12 17:51:48 workload [&{0xc420088780 /tmp/nodeagent/65dd5566-101d-11e8-a41f-42010af002c2/server.sock 0xc42006c150 0xc420164920}] listen
2018/02/12 17:52:41 Closed the listener.
2018/02/12 17:53:20 workload [&{0xc420088b40 /tmp/nodeagent/9c7f5c07-101d-11e8-a41f-42010af002c2/server.sock 0xc42006c1c0 0xc420164920}] listen
2018/02/12 17:53:20 [&{}]: &CheckRequest{Name:foo,} Check called
2018/02/12 17:53:20 Credentials are {9c7f5c07-101d-11e8-a41f-42010af002c2 udsver-client-5b99c548fd-64hrz default default <nil>}
...
```

From the output above you can see node agent is able to get the workload's uid info when receives requests from it.
