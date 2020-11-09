# Istio Node Readiness Controller

This package is used to add readiness taint to prevent race condition
during critical daemonset pod installation

## How it works

It will load the configmap defined by the user which contains
the information of critical labels and their namespaces
Controller will monitoring on all nodes and pods with critical labels
and namespaces defined in the configuration map
it will taint the node if some of the critical labels
are not setup in current node
and when all critical labels in given node is set up.
it will untaint the node to allow non-critical pods to register

## What it will do and not do

It is a complementary package to repair controller
because repair controller itself cannot prevent daemonset failure
and when istio-cni daemonset becomes unready,
it is not able to install iptable rules to pods and introduce race condition
thus it **must work together with istio-cni-repair controller**

It support much more generalized setting in node readiness
checking, thus user can define their own configuration maps for
more complicated readiness Check

## How to use

### create a configmap to define critical labels and their namespace

Configmap defines the namespace and label selector for critical pod,
and in default it should be located in kube-system namespace with
name node.readiness to let controller find them automatically
An example of configmap
Layout

```bash
./
configs/
   config
```

```bash
config file

- name: istio-cni
  selector: app=istio
  namespace: kube-system
```

command to create the configmap
sample output of configmap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: "istio-cni-taint"
  namespace: "kube-system"
  labels:
    app: istio-cni
data:
  config: |
    - name: istio-cni
      selector: app=istio
      namespace: kube-system
```

supports multi label in one

 ```yaml
 apiVersion: v1
 kind: ConfigMap
 metadata:
   name: "istio-cni-taint"
   namespace: "kube-system"
   labels:
     app: istio-cni
 data:
   config: |
     - name: istio-cni
       selector: app=istio, app=istio-cni
       namespace: kube-system
 ```

### config the critical pods and add node readiness tolerations to it

```yaml
Kind: Daemonset
Metadata:
    Name: istio-critical-pod
    Labels:
        app: istio
Spec:
  ...more...
    Toleration:
        Key: NodeReadiness
        Operator: Exists
        Effect: NoSchedule
```

### build it as binary

the command line interface is in `cni/cmd/istio-cni-taint/main.go`
using command

```bash
make istioctl
```

it will generate the binary version of command-line interface controller

### run command line interface for debugging and tests

find the istio-cni-taint binary in your output directory
run the following command to start controller

```bash
istio-cni-taint
```

If you want to customize nodes' readiness taint you should taint them by yourself

```bash
kubectl taint nodes <node-name> NodeReadiness:NoSchedule
```

and you need to set `--register-with-taints` option in kubelet to set
readiness taint to newly added node

```bash
kubelet --register-with-taints=NodeReadiness:NoSchedule
```
