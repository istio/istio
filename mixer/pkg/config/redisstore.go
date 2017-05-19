// Copyright 2017 Istio Authors
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

package config

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/golang/glog"
	"github.com/mediocregopher/radix.v2/redis"
)

type redisStore struct {
	client *redis.Client

	// The URL connecting to the database.
	url *url.URL

	// listLength caches the number of keys returned for List() to
	// reduce the number of allocations for similar quries.
	listLength int
}

func setupConnection(client *redis.Client, password string, dbNum uint64) error {
	if len(password) > 0 {
		resp := client.Cmd("AUTH", password)
		if resp.Err != nil {
			return fmt.Errorf("failed to authenticate with password %s: %v", password, resp.Err)
		}
	}

	// Invoke PING to make sure the client can emit commands properly.
	if resp := client.Cmd("PING"); resp.Err != nil {
		return resp.Err
	}

	if dbNum != 0 {
		// SELECT always returns okay, do not have to check the response.
		// See https://redis.io/commands/select
		client.Cmd("SELECT", dbNum)
	}
	return nil
}

func newRedisStore(u *url.URL) (rs *redisStore, err error) {
	var dbNum uint64
	if len(u.Path) > 1 {
		dbNum, err = strconv.ParseUint(u.Path[1:], 10, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to parse dbNum \"%s\", it should be an integer", u.Path[1:])
		}
	}

	client, err := redis.Dial("tcp", u.Host)
	if err != nil {
		return nil, fmt.Errorf("can't connect to the redis server %v: %v", u, err)
	}

	var password string
	if u.User != nil {
		password, _ = u.User.Password()
	}

	if err := setupConnection(client, password, dbNum); err != nil {
		return nil, err
	}
	return &redisStore{client, u, 0}, nil
}

func (rs *redisStore) String() string {
	return fmt.Sprintf("redisStore: %v", rs.url)
}

func (rs *redisStore) Get(key string) (value string, index int, found bool) {
	resp := rs.client.Cmd("GET", key)
	index = indexNotSupported
	if resp.Err != nil {

		return "", index, false
	}
	s, err := resp.Str()
	if err != nil {
		return "", index, false
	}
	return s, index, true
}

func (rs *redisStore) Set(key, value string) (index int, err error) {
	resp := rs.client.Cmd("SET", key, value)
	if resp.Err != nil {
		return indexNotSupported, err
	}
	return indexNotSupported, nil
}

func (rs *redisStore) List(key string, recurse bool) (keys []string, index int, err error) {
	keys = make([]string, 0, rs.listLength)
	keyPattern := key
	if key[len(key)-1] != '/' {
		keyPattern += "/"
	}
	keyPattern += "*"
	cursor := 0
	for {
		resp := rs.client.Cmd("SCAN", cursor, "MATCH", keyPattern)
		if resp.Err != nil {
			err = resp.Err
			break
		}
		resps, rerr := resp.Array()
		if rerr != nil {
			err = rerr
			break
		}
		if nextCursor, cerr := resps[0].Int(); cerr != nil {
			err = cerr
			break
		} else {
			cursor = nextCursor
		}
		respKeys, aerr := resps[1].Array()
		if aerr != nil {
			err = aerr
			break
		}
		for i, rk := range respKeys {
			// TODO: check recurse flag for filitering keys.
			if key, err2 := rk.Str(); err2 != nil {
				glog.Warningf("illformed responses %d-th value for cursor %d isn't a string (%v)", i, cursor, rk)
				continue
			} else {
				keys = append(keys, key)
			}
		}
		if cursor == 0 {
			break
		}
	}
	if err == nil {
		rs.listLength = len(keys)
	}
	return keys, indexNotSupported, err
}

func (rs *redisStore) Delete(key string) (err error) {
	return rs.client.Cmd("DEL", key).Err
}

func (rs *redisStore) Close() {
	if err := rs.client.Close(); err != nil {
		glog.Warningf("failed to close the connection: %v", err)
	}
}
