# k8sleaderelection

This directory is a fork of the [Kubernetes leader election package](https://github.com/kubernetes/kubernetes/tree/67fa728bcf795e864ef5a63fa4144ae044967581/staging/src/k8s.io/client-go/tools/leaderelection) that includes [prioritized leader election](https://github.com/kubernetes/kubernetes/pull/103442). These changes are required in order to make it so that the default revision wins leader elections and steals locks from non-default revisions. The changes were not upstreamed to k8s in time for the Istio 1.12 release.

Ideally, prioritized leader election gets upstreamed in the near future ([KEP](https://github.com/kubernetes/enhancements/pull/2836)), and hopefully what gets merged isn't too far from what we have forked here (if it is, we'd have to potentially maintain migration code in our fork for a few releases before abandoning). Once merged we can abandon this forked code and use upstream again.
