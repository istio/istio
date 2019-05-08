package miniredis

import (
	"sort"
	"strconv"
	"time"
)

func (db *RedisDB) exists(k string) bool {
	_, ok := db.keys[k]
	return ok
}

// t gives the type of a key, or ""
func (db *RedisDB) t(k string) string {
	return db.keys[k]
}

// allKeys returns all keys. Sorted.
func (db *RedisDB) allKeys() []string {
	res := make([]string, 0, len(db.keys))
	for k := range db.keys {
		res = append(res, k)
	}
	sort.Strings(res) // To make things deterministic.
	return res
}

// flush removes all keys and values.
func (db *RedisDB) flush() {
	db.keys = map[string]string{}
	db.stringKeys = map[string]string{}
	db.hashKeys = map[string]hashKey{}
	db.listKeys = map[string]listKey{}
	db.setKeys = map[string]setKey{}
	db.sortedsetKeys = map[string]sortedSet{}
	db.ttl = map[string]time.Duration{}
}

// move something to another db. Will return ok. Or not.
func (db *RedisDB) move(key string, to *RedisDB) bool {
	if _, ok := to.keys[key]; ok {
		return false
	}

	t, ok := db.keys[key]
	if !ok {
		return false
	}
	to.keys[key] = db.keys[key]
	switch t {
	case "string":
		to.stringKeys[key] = db.stringKeys[key]
	case "hash":
		to.hashKeys[key] = db.hashKeys[key]
	case "list":
		to.listKeys[key] = db.listKeys[key]
	case "set":
		to.setKeys[key] = db.setKeys[key]
	case "zset":
		to.sortedsetKeys[key] = db.sortedsetKeys[key]
	default:
		panic("unhandled key type")
	}
	to.keyVersion[key]++
	if v, ok := db.ttl[key]; ok {
		to.ttl[key] = v
	}
	db.del(key, true)
	return true
}

func (db *RedisDB) rename(from, to string) {
	db.del(to, true)
	switch db.t(from) {
	case "string":
		db.stringKeys[to] = db.stringKeys[from]
	case "hash":
		db.hashKeys[to] = db.hashKeys[from]
	case "list":
		db.listKeys[to] = db.listKeys[from]
	case "set":
		db.setKeys[to] = db.setKeys[from]
	case "zset":
		db.sortedsetKeys[to] = db.sortedsetKeys[from]
	default:
		panic("missing case")
	}
	db.keys[to] = db.keys[from]
	db.keyVersion[to]++
	db.ttl[to] = db.ttl[from]

	db.del(from, true)
}

func (db *RedisDB) del(k string, delTTL bool) {
	if !db.exists(k) {
		return
	}
	t := db.t(k)
	delete(db.keys, k)
	db.keyVersion[k]++
	if delTTL {
		delete(db.ttl, k)
	}
	switch t {
	case "string":
		delete(db.stringKeys, k)
	case "hash":
		delete(db.hashKeys, k)
	case "list":
		delete(db.listKeys, k)
	case "set":
		delete(db.setKeys, k)
	case "zset":
		delete(db.sortedsetKeys, k)
	default:
		panic("Unknown key type: " + t)
	}
}

// stringGet returns the string key or "" on error/nonexists.
func (db *RedisDB) stringGet(k string) string {
	if t, ok := db.keys[k]; !ok || t != "string" {
		return ""
	}
	return db.stringKeys[k]
}

// stringSet force set()s a key. Does not touch expire.
func (db *RedisDB) stringSet(k, v string) {
	db.del(k, false)
	db.keys[k] = "string"
	db.stringKeys[k] = v
	db.keyVersion[k]++
}

// change int key value
func (db *RedisDB) stringIncr(k string, delta int) (int, error) {
	v := 0
	if sv, ok := db.stringKeys[k]; ok {
		var err error
		v, err = strconv.Atoi(sv)
		if err != nil {
			return 0, ErrIntValueError
		}
	}
	v += delta
	db.stringSet(k, strconv.Itoa(v))
	return v, nil
}

// change float key value
func (db *RedisDB) stringIncrfloat(k string, delta float64) (float64, error) {
	v := 0.0
	if sv, ok := db.stringKeys[k]; ok {
		var err error
		v, err = strconv.ParseFloat(sv, 64)
		if err != nil {
			return 0, ErrFloatValueError
		}
	}
	v += delta
	db.stringSet(k, formatFloat(v))
	return v, nil
}

// listLpush is 'left push', aka unshift. Returns the new length.
func (db *RedisDB) listLpush(k, v string) int {
	l, ok := db.listKeys[k]
	if !ok {
		db.keys[k] = "list"
	}
	l = append([]string{v}, l...)
	db.listKeys[k] = l
	db.keyVersion[k]++
	return len(l)
}

// 'left pop', aka shift.
func (db *RedisDB) listLpop(k string) string {
	l := db.listKeys[k]
	el := l[0]
	l = l[1:]
	if len(l) == 0 {
		db.del(k, true)
	} else {
		db.listKeys[k] = l
	}
	db.keyVersion[k]++
	return el
}

func (db *RedisDB) listPush(k string, v ...string) int {
	l, ok := db.listKeys[k]
	if !ok {
		db.keys[k] = "list"
	}
	l = append(l, v...)
	db.listKeys[k] = l
	db.keyVersion[k]++
	return len(l)
}

func (db *RedisDB) listPop(k string) string {
	l := db.listKeys[k]
	el := l[len(l)-1]
	l = l[:len(l)-1]
	if len(l) == 0 {
		db.del(k, true)
	} else {
		db.listKeys[k] = l
		db.keyVersion[k]++
	}
	return el
}

// setset replaces a whole set.
func (db *RedisDB) setSet(k string, set setKey) {
	db.keys[k] = "set"
	db.setKeys[k] = set
	db.keyVersion[k]++
}

// setadd adds members to a set. Returns nr of new keys.
func (db *RedisDB) setAdd(k string, elems ...string) int {
	s, ok := db.setKeys[k]
	if !ok {
		s = setKey{}
		db.keys[k] = "set"
	}
	added := 0
	for _, e := range elems {
		if _, ok := s[e]; !ok {
			added++
		}
		s[e] = struct{}{}
	}
	db.setKeys[k] = s
	db.keyVersion[k]++
	return added
}

// setrem removes members from a set. Returns nr of deleted keys.
func (db *RedisDB) setRem(k string, fields ...string) int {
	s, ok := db.setKeys[k]
	if !ok {
		return 0
	}
	removed := 0
	for _, f := range fields {
		if _, ok := s[f]; ok {
			removed++
			delete(s, f)
		}
	}
	if len(s) == 0 {
		db.del(k, true)
	} else {
		db.setKeys[k] = s
	}
	db.keyVersion[k]++
	return removed
}

// All members of a set.
func (db *RedisDB) setMembers(k string) []string {
	set := db.setKeys[k]
	members := make([]string, 0, len(set))
	for k := range set {
		members = append(members, k)
	}
	sort.Strings(members)
	return members
}

// Is a SET value present?
func (db *RedisDB) setIsMember(k, v string) bool {
	set, ok := db.setKeys[k]
	if !ok {
		return false
	}
	_, ok = set[v]
	return ok
}

// hashFields returns all (sorted) keys ('fields') for a hash key.
func (db *RedisDB) hashFields(k string) []string {
	v := db.hashKeys[k]
	r := make([]string, 0, len(v))
	for k := range v {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}

// hashGet a value
func (db *RedisDB) hashGet(key, field string) string {
	return db.hashKeys[key][field]
}

// hashSet returns whether the key already existed
func (db *RedisDB) hashSet(k, f, v string) bool {
	if t, ok := db.keys[k]; ok && t != "hash" {
		db.del(k, true)
	}
	db.keys[k] = "hash"
	if _, ok := db.hashKeys[k]; !ok {
		db.hashKeys[k] = map[string]string{}
	}
	_, ok := db.hashKeys[k][f]
	db.hashKeys[k][f] = v
	db.keyVersion[k]++
	return ok
}

// hashIncr changes int key value
func (db *RedisDB) hashIncr(key, field string, delta int) (int, error) {
	v := 0
	if h, ok := db.hashKeys[key]; ok {
		if f, ok := h[field]; ok {
			var err error
			v, err = strconv.Atoi(f)
			if err != nil {
				return 0, ErrIntValueError
			}
		}
	}
	v += delta
	db.hashSet(key, field, strconv.Itoa(v))
	return v, nil
}

// hashIncrfloat changes float key value
func (db *RedisDB) hashIncrfloat(key, field string, delta float64) (float64, error) {
	v := 0.0
	if h, ok := db.hashKeys[key]; ok {
		if f, ok := h[field]; ok {
			var err error
			v, err = strconv.ParseFloat(f, 64)
			if err != nil {
				return 0, ErrFloatValueError
			}
		}
	}
	v += delta
	db.hashSet(key, field, formatFloat(v))
	return v, nil
}

// sortedSet set returns a sortedSet as map
func (db *RedisDB) sortedSet(key string) map[string]float64 {
	ss := db.sortedsetKeys[key]
	return map[string]float64(ss)
}

// ssetSet sets a complete sorted set.
func (db *RedisDB) ssetSet(key string, sset sortedSet) {
	db.keys[key] = "zset"
	db.keyVersion[key]++
	db.sortedsetKeys[key] = sset
}

// ssetAdd adds member to a sorted set. Returns whether this was a new member.
func (db *RedisDB) ssetAdd(key string, score float64, member string) bool {
	ss, ok := db.sortedsetKeys[key]
	if !ok {
		ss = newSortedSet()
		db.keys[key] = "zset"
	}
	_, ok = ss[member]
	ss[member] = score
	db.sortedsetKeys[key] = ss
	db.keyVersion[key]++
	return !ok
}

// All members from a sorted set, ordered by score.
func (db *RedisDB) ssetMembers(key string) []string {
	ss, ok := db.sortedsetKeys[key]
	if !ok {
		return nil
	}
	elems := ss.byScore(asc)
	members := make([]string, 0, len(elems))
	for _, e := range elems {
		members = append(members, e.member)
	}
	return members
}

// All members+scores from a sorted set, ordered by score.
func (db *RedisDB) ssetElements(key string) ssElems {
	ss, ok := db.sortedsetKeys[key]
	if !ok {
		return nil
	}
	return ss.byScore(asc)
}

// ssetCard is the sorted set cardinality.
func (db *RedisDB) ssetCard(key string) int {
	ss := db.sortedsetKeys[key]
	return ss.card()
}

// ssetRank is the sorted set rank.
func (db *RedisDB) ssetRank(key, member string, d direction) (int, bool) {
	ss := db.sortedsetKeys[key]
	return ss.rankByScore(member, d)
}

// ssetScore is sorted set score.
func (db *RedisDB) ssetScore(key, member string) float64 {
	ss := db.sortedsetKeys[key]
	return ss[member]
}

// ssetRem is sorted set key delete.
func (db *RedisDB) ssetRem(key, member string) bool {
	ss := db.sortedsetKeys[key]
	_, ok := ss[member]
	delete(ss, member)
	if len(ss) == 0 {
		// Delete key on removal of last member
		db.del(key, true)
	}
	return ok
}

// ssetExists tells if a member exists in a sorted set.
func (db *RedisDB) ssetExists(key, member string) bool {
	ss := db.sortedsetKeys[key]
	_, ok := ss[member]
	return ok
}

// ssetIncrby changes float sorted set score.
func (db *RedisDB) ssetIncrby(k, m string, delta float64) float64 {
	ss, ok := db.sortedsetKeys[k]
	if !ok {
		ss = newSortedSet()
		db.keys[k] = "zset"
		db.sortedsetKeys[k] = ss
	}

	v, _ := ss.get(m)
	v += delta
	ss.set(v, m)
	db.keyVersion[k]++
	return v
}

// setDiff implements the logic behind SDIFF*
func (db *RedisDB) setDiff(keys []string) (setKey, error) {
	key := keys[0]
	keys = keys[1:]
	if db.exists(key) && db.t(key) != "set" {
		return nil, ErrWrongType
	}
	s := setKey{}
	for k := range db.setKeys[key] {
		s[k] = struct{}{}
	}
	for _, sk := range keys {
		if !db.exists(sk) {
			continue
		}
		if db.t(sk) != "set" {
			return nil, ErrWrongType
		}
		for e := range db.setKeys[sk] {
			delete(s, e)
		}
	}
	return s, nil
}

// setInter implements the logic behind SINTER*
func (db *RedisDB) setInter(keys []string) (setKey, error) {
	key := keys[0]
	keys = keys[1:]
	if db.exists(key) && db.t(key) != "set" {
		return nil, ErrWrongType
	}
	s := setKey{}
	for k := range db.setKeys[key] {
		s[k] = struct{}{}
	}
	for _, sk := range keys {
		if !db.exists(sk) {
			continue
		}
		if db.t(sk) != "set" {
			// Bug(?) in redis 2.8.14, it just skips the key.
			continue
			// return nil, ErrWrongType
		}
		other := db.setKeys[sk]
		for e := range s {
			if _, ok := other[e]; ok {
				continue
			}
			delete(s, e)
		}
	}
	return s, nil
}

// setUnion implements the logic behind SUNION*
func (db *RedisDB) setUnion(keys []string) (setKey, error) {
	key := keys[0]
	keys = keys[1:]
	if db.exists(key) && db.t(key) != "set" {
		return nil, ErrWrongType
	}
	s := setKey{}
	for k := range db.setKeys[key] {
		s[k] = struct{}{}
	}
	for _, sk := range keys {
		if !db.exists(sk) {
			continue
		}
		if db.t(sk) != "set" {
			return nil, ErrWrongType
		}
		for e := range db.setKeys[sk] {
			s[e] = struct{}{}
		}
	}
	return s, nil
}

// fastForward proceeds the current timestamp with duration, works as a time machine
func (db *RedisDB) fastForward(duration time.Duration) {
	for _, key := range db.allKeys() {
		if value, ok := db.ttl[key]; ok {
			db.ttl[key] = value - duration
			db.checkTTL(key)
		}
	}
}

func (db *RedisDB) checkTTL(key string) {
	if v, ok := db.ttl[key]; ok && v <= 0 {
		db.del(key, true)
	}
}
