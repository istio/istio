/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Package event contains the definitions for the Event types produced by source.Sources and transformed into
reconcile.Requests by handler.EventHandler.

The details of how events are produced and transformed into reconcile.Requests are not something most
users should need to use or understand.  Instead of working with Events, users should use
source.Sources and handler.EventHandlers with Controller.Watch.
*/
package event
