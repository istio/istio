# Running and debugging the controller

## Prep the cluster

```bash
kubectl apply -f mdp/controller/manifests/cluster-resources.yaml
kubectl apply -f mdp/controller/manifests/cr.yaml
```

## Running from CLI

```bash
go run ./mdp/controller/cmd/... server
```

## Running from Goland debugger

Need to install clang.

```bash
sudo apt-get install clang
```
