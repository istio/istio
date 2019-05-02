// Copyright 2018 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package leader implements Leader For Life, a simple alternative to lease-based
leader election.

Both the Leader For Life and lease-based approaches to leader election are
built on the concept that each candidate will attempt to create a resource with
the same GVK, namespace, and name. Whichever candidate succeeds becomes the
leader. The rest receive "already exists" errors and wait for a new
opportunity.

Leases provide a way to indirectly observe whether the leader still exists. The
leader must periodically renew its lease, usually by updating a timestamp in
its lock record. If it fails to do so, it is presumed dead, and a new election
takes place. If the leader is in fact still alive but unreachable, it is
expected to gracefully step down. A variety of factors can cause a leader to
fail at updating its lease, but continue acting as the leader before succeeding
at stepping down.

In the "leader for life" approach, a specific Pod is the leader. Once
established (by creating a lock record), the Pod is the leader until it is
destroyed. There is no possibility for multiple pods to think they are the
leader at the same time. The leader does not need to renew a lease, consider
stepping down, or do anything related to election activity once it becomes the
leader.

The lock record in this case is a ConfigMap whose OwnerReference is set to the
Pod that is the leader. When the leader is destroyed, the ConfigMap gets
garbage-collected, enabling a different candidate Pod to become the leader.

Leader for Life requires that all candidate Pods be in the same Namespace. It
uses the downwards API to determine the pod name, as hostname is not reliable.
You should run it configured with:

env:
  - name: POD_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
*/
package leader
