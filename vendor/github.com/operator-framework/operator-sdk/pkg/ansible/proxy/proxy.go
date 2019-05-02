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

package proxy

// This file contains this project's custom code, as opposed to kubectl.go
// which contains code retrieved from the kubernetes project.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/operator-framework/operator-sdk/pkg/ansible/proxy/controllermap"
	"github.com/operator-framework/operator-sdk/pkg/ansible/proxy/kubeconfig"
	k8sRequest "github.com/operator-framework/operator-sdk/pkg/ansible/proxy/requestfactory"
	osdkHandler "github.com/operator-framework/operator-sdk/pkg/handler"
	"k8s.io/apimachinery/pkg/api/meta"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type marshaler interface {
	MarshalJSON() ([]byte, error)
}

// CacheResponseHandler will handle proxied requests and check if the requested
// resource exists in our cache. If it does then there is no need to bombard
// the APIserver with our request and we should write the response from the
// proxy.
func CacheResponseHandler(h http.Handler, informerCache cache.Cache, restMapper meta.RESTMapper, watchedNamespaces map[string]interface{}, cMap *controllermap.ControllerMap, injectOwnerRef bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			// GET request means we need to check the cache
			rf := k8sRequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
			r, err := rf.NewRequestInfo(req)
			if err != nil {
				log.Error(err, "Failed to convert request")
				break
			}

			// check if resource is present on request
			if !r.IsResourceRequest {
				break
			}

			// check if resource doesn't exist in watched namespaces
			// if watchedNamespaces[""] exists then we are watching all namespaces
			// and want to continue
			_, allNsPresent := watchedNamespaces[metav1.NamespaceAll]
			_, reqNsPresent := watchedNamespaces[r.Namespace]
			if !allNsPresent && !reqNsPresent {
				break
			}

			if strings.HasPrefix(r.Path, "/version") {
				// Temporarily pass along to API server
				// Ideally we cache this response as well
				break
			}

			gvr := schema.GroupVersionResource{
				Group:    r.APIGroup,
				Version:  r.APIVersion,
				Resource: r.Resource,
			}
			if restMapper == nil {
				restMapper = meta.NewDefaultRESTMapper([]schema.GroupVersion{schema.GroupVersion{
					Group:   r.APIGroup,
					Version: r.APIVersion,
				}})
			}

			k, err := restMapper.KindFor(gvr)
			if err != nil {
				// break here in case resource doesn't exist in cache
				log.Info("Cache miss, can not find in rest mapper", "GVR", gvr)
				break
			}

			var m marshaler

			log.V(2).Info("Get resource in our cache", "r", r)
			if r.Verb == "list" {
				listOptions := &metav1.ListOptions{}
				if err := metainternalversion.ParameterCodec.DecodeParameters(req.URL.Query(), metav1.SchemeGroupVersion, listOptions); err != nil {
					log.Error(err, "Unable to decode list options from request")
					break
				}
				lo := client.InNamespace(r.Namespace)
				if err := lo.SetLabelSelector(listOptions.LabelSelector); err != nil {
					log.Error(err, "Unable to set label selectors for the client")
					break
				}
				if listOptions.FieldSelector != "" {
					if err := lo.SetFieldSelector(listOptions.FieldSelector); err != nil {
						log.Error(err, "Unable to set field selectors for the client")
						break
					}
				}
				k.Kind = k.Kind + "List"
				un := unstructured.UnstructuredList{}
				un.SetGroupVersionKind(k)
				err = informerCache.List(context.Background(), lo, &un)
				if err != nil {
					// break here in case resource doesn't exist in cache but exists on APIserver
					// This is very unlikely but provides user with expected 404
					log.Info(fmt.Sprintf("cache miss: %v err-%v", k, err))
					break
				}
				m = &un
			} else {
				un := &unstructured.Unstructured{}
				un.SetGroupVersionKind(k)
				obj := client.ObjectKey{Namespace: r.Namespace, Name: r.Name}
				err = informerCache.Get(context.Background(), obj, un)
				if err != nil {
					// break here in case resource doesn't exist in cache but exists on APIserver
					// This is very unlikely but provides user with expected 404
					log.Info(fmt.Sprintf("Cache miss: %v, %v", k, obj))
					break
				}
				m = un
				// Once we get the resource, we are going to attempt to recover the dependent watches here,
				// This will happen in the background, and log errors.
				if injectOwnerRef {
					go recoverDependentWatches(req, un, cMap, restMapper)
				}
			}

			i := bytes.Buffer{}
			resp, err := m.MarshalJSON()
			if err != nil {
				// return will give a 500
				log.Error(err, "Failed to marshal data")
				http.Error(w, "", http.StatusInternalServerError)
				return
			}

			// Set Content-Type header
			w.Header().Set("Content-Type", "application/json")
			// Set X-Cache header to signal that response is served from Cache
			w.Header().Set("X-Cache", "HIT")
			if err := json.Indent(&i, resp, "", "  "); err != nil {
				log.Error(err, "Failed to indent json")
			}
			_, err = w.Write(i.Bytes())
			if err != nil {
				log.Error(err, "Failed to write response")
				http.Error(w, "", http.StatusInternalServerError)
				return
			}

			// Return so that request isn't passed along to APIserver
			return
		}
		h.ServeHTTP(w, req)
	})
}

func recoverDependentWatches(req *http.Request, un *unstructured.Unstructured, cMap *controllermap.ControllerMap, restMapper meta.RESTMapper) {
	ownerRef, err := getRequestOwnerRef(req)
	if err != nil {
		log.Error(err, "Could not get ownerRef from proxy")
		return
	}

	for _, oRef := range un.GetOwnerReferences() {
		if oRef.APIVersion == ownerRef.APIVersion && oRef.Kind == ownerRef.Kind {
			err := addWatchToController(ownerRef, cMap, un, restMapper, true)
			if err != nil {
				log.Error(err, "Could not recover dependent resource watch", "owner", ownerRef)
				return
			}
		}
	}
	if typeString, ok := un.GetAnnotations()[osdkHandler.TypeAnnotation]; ok {
		ownerGV, err := schema.ParseGroupVersion(ownerRef.APIVersion)
		if err != nil {
			log.Error(err, "Could not get ownerRef from proxy")
			return
		}
		if typeString == fmt.Sprintf("%v.%v", ownerRef.Kind, ownerGV.Group) {
			err := addWatchToController(ownerRef, cMap, un, restMapper, false)
			if err != nil {
				log.Error(err, "Could not recover dependent resource watch", "owner", ownerRef)
				return
			}
		}
	}
}

// InjectOwnerReferenceHandler will handle proxied requests and inject the
// owner reference found in the authorization header. The Authorization is
// then deleted so that the proxy can re-set with the correct authorization.
func InjectOwnerReferenceHandler(h http.Handler, cMap *controllermap.ControllerMap, restMapper meta.RESTMapper, watchedNamespaces map[string]interface{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodPost:
			dump, _ := httputil.DumpRequest(req, false)
			log.V(1).Info("Dumping request", "RequestDump", string(dump))
			rf := k8sRequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
			r, err := rf.NewRequestInfo(req)
			if err != nil {
				m := "Could not convert request"
				log.Error(err, m)
				http.Error(w, m, http.StatusBadRequest)
				return
			}
			if r.Subresource != "" {
				// Don't inject owner ref if we are POSTing to a subresource
				break
			}
			log.Info("Injecting owner reference")
			owner, err := getRequestOwnerRef(req)
			if err != nil {
				m := "Could not get owner reference"
				log.Error(err, m)
				http.Error(w, m, http.StatusInternalServerError)
				return
			}

			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				m := "Could not read request body"
				log.Error(err, m)
				http.Error(w, m, http.StatusInternalServerError)
				return
			}
			data := &unstructured.Unstructured{}
			err = json.Unmarshal(body, data)
			if err != nil {
				m := "Could not deserialize request body"
				log.Error(err, m)
				http.Error(w, m, http.StatusBadRequest)
				return
			}

			addOwnerRef, err := shouldAddOwnerRef(data, owner, restMapper)
			if err != nil {
				m := "Could not determine if we should add owner ref"
				log.Error(err, m)
				http.Error(w, m, http.StatusBadRequest)
				return
			}
			if addOwnerRef {
				data.SetOwnerReferences(append(data.GetOwnerReferences(), owner.OwnerReference))
			} else {
				ownerGV, err := schema.ParseGroupVersion(owner.APIVersion)
				if err != nil {
					m := fmt.Sprintf("could not get broup version for: %v", owner)
					log.Error(err, m)
					http.Error(w, m, http.StatusBadRequest)
					return
				}
				a := data.GetAnnotations()
				if a == nil {
					a = map[string]string{}
				}
				a[osdkHandler.NamespacedNameAnnotation] = strings.Join([]string{owner.Namespace, owner.Name}, "/")
				a[osdkHandler.TypeAnnotation] = fmt.Sprintf("%v.%v", owner.Kind, ownerGV.Group)

				data.SetAnnotations(a)
			}
			newBody, err := json.Marshal(data.Object)
			if err != nil {
				m := "Could not serialize body"
				log.Error(err, m)
				http.Error(w, m, http.StatusInternalServerError)
				return
			}
			log.V(1).Info("Serialized body", "Body", string(newBody))
			req.Body = ioutil.NopCloser(bytes.NewBuffer(newBody))
			req.ContentLength = int64(len(newBody))

			// add watch for resource
			// check if resource doesn't exist in watched namespaces
			// if watchedNamespaces[""] exists then we are watching all namespaces
			// and want to continue
			// This is making sure we are not attempting to watch a resource outside of the
			// namespaces that the cache can watch.
			_, allNsPresent := watchedNamespaces[metav1.NamespaceAll]
			_, reqNsPresent := watchedNamespaces[r.Namespace]
			if allNsPresent || reqNsPresent {
				err = addWatchToController(owner, cMap, data, restMapper, addOwnerRef)
				if err != nil {
					m := "could not add watch to controller"
					log.Error(err, m)
					http.Error(w, m, http.StatusInternalServerError)
					return
				}
			}
		}
		h.ServeHTTP(w, req)
	})
}

func removeAuthorizationHeader(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Header.Del("Authorization")
		h.ServeHTTP(w, req)
	})
}

func shouldAddOwnerRef(data *unstructured.Unstructured, owner kubeconfig.NamespacedOwnerReference, restMapper meta.RESTMapper) (bool, error) {
	dataMapping, err := restMapper.RESTMapping(data.GroupVersionKind().GroupKind(), data.GroupVersionKind().Version)
	if err != nil {
		m := fmt.Sprintf("Could not get rest mapping for: %v", data.GroupVersionKind())
		log.Error(err, m)
		return false, err

	}
	// We need to determine whether or not the owner is a cluster-scoped
	// resource because enqueue based on an owner reference does not work if
	// a namespaced resource owns a cluster-scoped resource
	ownerGV, err := schema.ParseGroupVersion(owner.APIVersion)
	if err != nil {
		m := fmt.Sprintf("could not get group version for: %v", owner)
		log.Error(err, m)
		return false, err
	}
	ownerMapping, err := restMapper.RESTMapping(schema.GroupKind{Kind: owner.Kind, Group: ownerGV.Group}, ownerGV.Version)
	if err != nil {
		m := fmt.Sprintf("could not get rest mapping for: %v", owner)
		log.Error(err, m)
		return false, err
	}

	dataNamespaceScoped := dataMapping.Scope.Name() != meta.RESTScopeNameRoot
	ownerNamespaceScoped := ownerMapping.Scope.Name() != meta.RESTScopeNameRoot

	if dataNamespaceScoped && ownerNamespaceScoped && data.GetNamespace() == owner.Namespace {
		return true, nil
	}
	return false, nil
}

// RequestLogHandler - log the requests that come through the proxy.
func RequestLogHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// read body
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			log.Error(err, "Could not read request body")
		}
		// fix body
		req.Body = ioutil.NopCloser(bytes.NewBuffer(body))
		log.Info("Request Info", "method", req.Method, "uri", req.RequestURI, "body", string(body))
		// Removing the authorization so that the proxy can set the correct authorization.
		req.Header.Del("Authorization")
		h.ServeHTTP(w, req)
	})
}

// HandlerChain will be used for users to pass defined handlers to the proxy.
// The hander chain will be run after InjectingOwnerReference if it is added
// and before the proxy handler.
type HandlerChain func(http.Handler) http.Handler

// Options will be used by the user to specify the desired details
// for the proxy.
type Options struct {
	Address           string
	Port              int
	Handler           HandlerChain
	OwnerInjection    bool
	LogRequests       bool
	KubeConfig        *rest.Config
	Cache             cache.Cache
	RESTMapper        meta.RESTMapper
	ControllerMap     *controllermap.ControllerMap
	WatchedNamespaces []string
	DisableCache      bool
}

// Run will start a proxy server in a go routine that returns on the error
// channel if something is not correct on startup. Run will not return until
// the network socket is listening.
func Run(done chan error, o Options) error {
	server, err := newServer("/", o.KubeConfig)
	if err != nil {
		return err
	}
	if o.Handler != nil {
		server.Handler = o.Handler(server.Handler)
	}
	if o.ControllerMap == nil {
		return fmt.Errorf("failed to get controller map from options")
	}
	if o.WatchedNamespaces == nil {
		return fmt.Errorf("failed to get list of watched namespaces from options")
	}

	watchedNamespaceMap := make(map[string]interface{})
	// Convert string list to map
	for _, ns := range o.WatchedNamespaces {
		watchedNamespaceMap[ns] = nil
	}

	if o.Cache == nil && !o.DisableCache {
		// Need to initialize cache since we don't have one
		log.Info("Initializing and starting informer cache...")
		informerCache, err := cache.New(o.KubeConfig, cache.Options{})
		if err != nil {
			return err
		}
		stop := make(chan struct{})
		go func() {
			if err := informerCache.Start(stop); err != nil {
				log.Error(err, "Failed to start informer cache")
			}
			defer close(stop)
		}()
		log.Info("Waiting for cache to sync...")
		synced := informerCache.WaitForCacheSync(stop)
		if !synced {
			return fmt.Errorf("failed to sync cache")
		}
		log.Info("Cache sync was successful")
		o.Cache = informerCache
	}

	server.Handler = removeAuthorizationHeader(server.Handler)

	if o.OwnerInjection {
		server.Handler = InjectOwnerReferenceHandler(server.Handler, o.ControllerMap, o.RESTMapper, watchedNamespaceMap)
	} else {
		log.Info("Warning: injection of owner references and dependent watches is turned off")
	}
	if o.LogRequests {
		server.Handler = RequestLogHandler(server.Handler)
	}
	if !o.DisableCache {
		server.Handler = CacheResponseHandler(server.Handler, o.Cache, o.RESTMapper, watchedNamespaceMap, o.ControllerMap, o.OwnerInjection)
	}

	l, err := server.Listen(o.Address, o.Port)
	if err != nil {
		return err
	}
	go func() {
		log.Info("Starting to serve", "Address", l.Addr().String())
		done <- server.ServeOnListener(l)
	}()
	return nil
}

func addWatchToController(owner kubeconfig.NamespacedOwnerReference, cMap *controllermap.ControllerMap, resource *unstructured.Unstructured, restMapper meta.RESTMapper, useOwnerRef bool) error {
	dataMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
	if err != nil {
		m := fmt.Sprintf("Could not get rest mapping for: %v", resource.GroupVersionKind())
		log.Error(err, m)
		return err

	}
	ownerGV, err := schema.ParseGroupVersion(owner.APIVersion)
	if err != nil {
		m := fmt.Sprintf("could not get broup version for: %v", owner)
		log.Error(err, m)
		return err
	}
	ownerMapping, err := restMapper.RESTMapping(schema.GroupKind{Kind: owner.Kind, Group: ownerGV.Group}, ownerGV.Version)
	if err != nil {
		m := fmt.Sprintf("could not get rest mapping for: %v", owner)
		log.Error(err, m)
		return err
	}

	dataNamespaceScoped := dataMapping.Scope.Name() != meta.RESTScopeNameRoot
	contents, ok := cMap.Get(ownerMapping.GroupVersionKind)
	if !ok {
		return errors.New("failed to find controller in map")
	}
	owMap := contents.OwnerWatchMap
	awMap := contents.AnnotationWatchMap
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(ownerMapping.GroupVersionKind)
	// Add a watch to controller
	if contents.WatchDependentResources {
		// Store watch in map
		// Use EnqueueRequestForOwner unless user has configured watching cluster scoped resources and we have to
		switch {
		case useOwnerRef:
			_, exists := owMap.Get(resource.GroupVersionKind())
			// If already watching resource no need to add a new watch
			if exists {
				return nil
			}

			owMap.Store(resource.GroupVersionKind())
			log.Info("Watching child resource", "kind", resource.GroupVersionKind(), "enqueue_kind", u.GroupVersionKind())
			// Store watch in map
			err := contents.Controller.Watch(&source.Kind{Type: resource}, &handler.EnqueueRequestForOwner{OwnerType: u})
			if err != nil {
				return err
			}
		case (!useOwnerRef && dataNamespaceScoped) || contents.WatchClusterScopedResources:
			_, exists := awMap.Get(resource.GroupVersionKind())
			// If already watching resource no need to add a new watch
			if exists {
				return nil
			}
			awMap.Store(resource.GroupVersionKind())
			typeString := fmt.Sprintf("%v.%v", owner.Kind, ownerGV.Group)
			log.Info("Watching child resource", "kind", resource.GroupVersionKind(), "enqueue_annotation_type", typeString)
			err = contents.Controller.Watch(&source.Kind{Type: resource}, &osdkHandler.EnqueueRequestForAnnotation{Type: typeString})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func getRequestOwnerRef(req *http.Request) (kubeconfig.NamespacedOwnerReference, error) {
	owner := kubeconfig.NamespacedOwnerReference{}
	user, _, ok := req.BasicAuth()
	if !ok {
		return owner, errors.New("basic auth header not found")
	}
	authString, err := base64.StdEncoding.DecodeString(user)
	if err != nil {
		m := "Could not base64 decode username"
		log.Error(err, m)
		return owner, err
	}
	// Set owner to NamespacedOwnerReference, which has metav1.OwnerReference
	// as a subset along with the Namespace of the owner. Please see the
	// kubeconfig.NamespacedOwnerReference type for more information. The
	// namespace is required when creating the reconcile requests.
	json.Unmarshal(authString, &owner)
	if err := json.Unmarshal(authString, &owner); err != nil {
		m := "Could not unmarshal auth string"
		log.Error(err, m)
		return owner, err
	}
	return owner, err
}
