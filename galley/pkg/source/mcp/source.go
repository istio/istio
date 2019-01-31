package mcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"

	mcpapi "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/mcp/client"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/sink"
)

var (
	scope = log.RegisterScope("mcp", "mcp debugging", 0)
)

// This should line up with the MCP client's batch size so we can enqueue every event in a batch without blocking.
const defaultChanSize = 1000

type source struct {
	nodeID             string
	address            string
	ctx                context.Context
	dial               func(ctx context.Context, address string) (mcpapi.AggregatedMeshConfigServiceClient, error)
	handler            resource.EventHandler
	stop               func()
	cashCollectionLock map[string]*sync.Mutex
	lCache             map[string]map[string]*cache
}

type cache struct {
	version string
	prevVer string
	ek      resource.EventKind
	entry   *resource.Entry
}

var _ runtime.Source = &source{}
var _ sink.Updater = &source{}

// New returns nil error. error is added to be consistent with other source's New functions
func New(ctx context.Context, copts *creds.Options, mcpAddress, nodeID string) (runtime.Source, error) {
	s := &source{
		nodeID:             nodeID,
		address:            mcpAddress,
		ctx:                ctx,
		cashCollectionLock: make(map[string]*sync.Mutex),
		lCache:             make(map[string]map[string]*cache),
		dial: func(ctx context.Context, address string) (mcpapi.AggregatedMeshConfigServiceClient, error) {

			// Copied from pilot/pkg/bootstrap/server.go
			requiredMCPCertCheckFreq := 500 * time.Millisecond
			u, err := url.Parse(mcpAddress)
			if err != nil {
				return nil, err
			}

			securityOption := grpc.WithInsecure()
			if u.Scheme == "mcps" {
				// TODO - Check for presence of cert files - this code could be extracted into function
				// and shared with pilot/pkg/bootstrap/server.go- initMCPConfigController
				// https://github.com/istio/istio/blob/master/pilot/pkg/bootstrap/server.go#L574-L591

				requiredFiles := []string{
					copts.CertificateFile,
					copts.KeyFile,
					copts.CACertificateFile,
				}
				scope.Infof("Secure MCP Client configured. Waiting for required certificate files to become available: %v",
					requiredFiles)
				retry := 20 //10s
				for len(requiredFiles) > 0 {
					if _, err := os.Stat(requiredFiles[0]); os.IsNotExist(err) {
						log.Infof("%v not found. Checking again in %v", requiredFiles[0], requiredMCPCertCheckFreq)
						select {
						case <-ctx.Done():
							return nil, nil
						case <-time.After(requiredMCPCertCheckFreq):
							if retry <= 0 {
								return nil, fmt.Errorf("required file %s not found", requiredFiles[0])
							}
							retry--
						}
						continue
					}
					log.Infof("%v found", requiredFiles[0])
					requiredFiles = requiredFiles[1:]
					retry = 20
				}

				watcher, err := creds.WatchFiles(ctx.Done(), copts)
				if err != nil {
					return nil, err
				}
				credentials := creds.CreateForClient(u.Hostname(), watcher)
				securityOption = grpc.WithTransportCredentials(credentials)
			}

			msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(bootstrap.DefaultMCPMaxMsgSize))
			conn, err := grpc.DialContext(ctx, u.Hostname()+":"+u.Port(), securityOption, msgSizeOption)
			if err != nil {
				scope.Errorf("Unable to dial MCP Server %q: %v", address, err)
				return nil, err
			}
			return mcpapi.NewAggregatedMeshConfigServiceClient(conn), nil
		},
	}
	return s, nil
}

func (s *source) Start(handler resource.EventHandler) error {
	//Assign the channel for the Apply function to use
	s.handler = handler
	ctx, cancel := context.WithCancel(s.ctx)
	s.stop = cancel
	cl, err := s.dial(ctx, s.address)
	if err != nil {
		scope.Debugf("failed to dial %q", s.address)
		return err
	}

	options := &sink.Options{
		CollectionOptions: sink.CollectionOptionsFromSlice(metadata.Types.Collections()),
		Updater:           s,
		ID:                s.nodeID,
		Metadata:          map[string]string{},
		Reporter:          monitoring.NewStatsContext("galley-mcp-client"),
	}
	mcpClient := client.New(cl, options)
	scope.Debugf("starting MCP client in its own goroutine")
	go mcpClient.Run(ctx)
	return nil
}

func (s *source) Stop() {
	s.stop()
}

func (s *source) Apply(c *sink.Change) error {
	cInfo, found := metadata.Types.Lookup(c.Collection)
	if !found {
		return fmt.Errorf("invalid type: %v", c.Collection)
	}
	scope.Debugf("received object %+v and length %d", c, len(c.Objects))

	if _, ok := s.lCache[c.Collection]; !ok {
		s.lCache[c.Collection] = make(map[string]*cache)
		s.cashCollectionLock[c.Collection] = &sync.Mutex{}
	}
	s.cashCollectionLock[c.Collection].Lock()
	defer s.cashCollectionLock[c.Collection].Unlock()

	// By default mark all the elements of local cache deleted, if present
	for _, c := range s.lCache[c.Collection] {
		c.ek = resource.Deleted
	}

	var errs error
	for _, o := range c.Objects {
		if o.TypeURL != cInfo.TypeURL.String() {
			errs = multierror.Append(errs,
				fmt.Errorf("type %v mismatch in received object: %v", cInfo.TypeURL.String(), o.TypeURL))
			continue
		}
		e, err := toEntry(o, cInfo)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		// If there is an error, ignore the entire batch.
		// but keep appending errs[] for the batch
		if errs == nil {
			lc, ok := s.lCache[c.Collection][o.Metadata.Name]
			if ok {
				if lc.version == o.Metadata.Version {
					lc.ek = resource.None
				} else {
					lc.prevVer = lc.version
					lc.version = o.Metadata.Version
					lc.ek = resource.Updated
				}
				lc.entry = &e

			} else {
				s.lCache[c.Collection][o.Metadata.Name] = &cache{
					version: o.Metadata.Version,
					ek:      resource.Added,
					entry:   &e,
				}
			}
		}
	}

	// If there is an error, the entire batch will be ignored
	// Hence remove any new adds from the local cache
	// And restore prev version if there were updates
	if errs != nil {
		for key, lc := range s.lCache[c.Collection] {
			if lc.ek == resource.Added {
				// this is safe because anything with state `Added` was new in this batch
				delete(s.lCache[c.Collection], key)
			} else if lc.ek == resource.Updated {
				lc.version = lc.prevVer // We are not worried about the entry as it will be overwritten
			}
		}
		return errs
	}

	// At this point we have all resources for that TypeURL
	//loop through the local cache and generate the appropriate events
	fsync := false
	for key, lc := range s.lCache[c.Collection] {
		if lc.ek == resource.None {
			continue
		}
		fsync = true
		scope.Debugf("pushed an event %+v", lc.ek)
		s.handler(resource.Event{
			Kind:  lc.ek,
			Entry: *lc.entry,
		})
		// If its a Deleted event, remove it from local cache
		if lc.ek == resource.Deleted {
			delete(s.lCache[c.Collection], key)
		}
	}
	if fsync {
		// Do a FullSynch after all events for a publish
		s.handler(resource.Event{Kind: resource.FullSync})
	}

	return nil
}

// toEntry converts the object into a resource.Entry.
func toEntry(o *sink.Object, info resource.Info) (resource.Entry, error) {

	t := time.Now()
	if o.Metadata.CreateTime != nil {
		var err error
		if t, err = types.TimestampFromProto(o.Metadata.CreateTime); err != nil {
			return resource.Entry{}, err
		}
	}
	// Verified in the caller that o is of kind info
	return resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				Collection: info.Collection,
				FullName:   resource.FullNameFromNamespaceAndName("", o.Metadata.Name),
			},
			Version: resource.Version(o.Metadata.Version),
		},
		Metadata: resource.Metadata{
			CreateTime:  t,
			Annotations: o.Metadata.Annotations,
			Labels:      o.Metadata.Labels,
		},
		Item: o.Body,
	}, nil
}
