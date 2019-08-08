#!/bin/bash
kubectl delete policy default overwrite-example -n foo
kubectl delete policy httpbin -n bar
kubectl delete destinationrules default overwrite-example -n foo
kubectl delete destinationrules httpbin -n bar