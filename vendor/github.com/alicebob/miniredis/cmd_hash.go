// Commands from http://redis.io/commands#hash

package miniredis

import (
	"strconv"
	"strings"

	"github.com/alicebob/miniredis/server"
)

// commandsHash handles all hash value operations.
func commandsHash(m *Miniredis) {
	m.srv.Register("HDEL", m.cmdHdel)
	m.srv.Register("HEXISTS", m.cmdHexists)
	m.srv.Register("HGET", m.cmdHget)
	m.srv.Register("HGETALL", m.cmdHgetall)
	m.srv.Register("HINCRBY", m.cmdHincrby)
	m.srv.Register("HINCRBYFLOAT", m.cmdHincrbyfloat)
	m.srv.Register("HKEYS", m.cmdHkeys)
	m.srv.Register("HLEN", m.cmdHlen)
	m.srv.Register("HMGET", m.cmdHmget)
	m.srv.Register("HMSET", m.cmdHmset)
	m.srv.Register("HSET", m.cmdHset)
	m.srv.Register("HSETNX", m.cmdHsetnx)
	m.srv.Register("HVALS", m.cmdHvals)
	m.srv.Register("HSCAN", m.cmdHscan)
}

// HSET
func (m *Miniredis) cmdHset(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, field, value := args[0], args[1], args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		if db.hashSet(key, field, value) {
			c.WriteInt(0)
		} else {
			c.WriteInt(1)
		}
	})
}

// HSETNX
func (m *Miniredis) cmdHsetnx(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, field, value := args[0], args[1], args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		if _, ok := db.hashKeys[key]; !ok {
			db.hashKeys[key] = map[string]string{}
			db.keys[key] = "hash"
		}
		_, ok := db.hashKeys[key][field]
		if ok {
			c.WriteInt(0)
			return
		}
		db.hashKeys[key][field] = value
		db.keyVersion[key]++
		c.WriteInt(1)
	})
}

// HMSET
func (m *Miniredis) cmdHmset(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, args := args[0], args[1:]
	if len(args)%2 != 0 {
		setDirty(c)
		// non-default error message
		c.WriteError("ERR wrong number of arguments for HMSET")
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		for len(args) > 0 {
			field, value := args[0], args[1]
			args = args[2:]
			db.hashSet(key, field, value)
		}
		c.WriteOK()
	})
}

// HGET
func (m *Miniredis) cmdHget(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, field := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteNull()
			return
		}
		if t != "hash" {
			c.WriteError(msgWrongType)
			return
		}
		value, ok := db.hashKeys[key][field]
		if !ok {
			c.WriteNull()
			return
		}
		c.WriteBulk(value)
	})
}

// HDEL
func (m *Miniredis) cmdHdel(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, fields := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			// No key is zero deleted
			c.WriteInt(0)
			return
		}
		if t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		deleted := 0
		for _, f := range fields {
			_, ok := db.hashKeys[key][f]
			if !ok {
				continue
			}
			delete(db.hashKeys[key], f)
			deleted++
		}
		c.WriteInt(deleted)

		// Nothing left. Remove the whole key.
		if len(db.hashKeys[key]) == 0 {
			db.del(key, true)
		}
	})
}

// HEXISTS
func (m *Miniredis) cmdHexists(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, field := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteInt(0)
			return
		}
		if t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		if _, ok := db.hashKeys[key][field]; !ok {
			c.WriteInt(0)
			return
		}
		c.WriteInt(1)
	})
}

// HGETALL
func (m *Miniredis) cmdHgetall(c *server.Peer, cmd string, args []string) {
	if len(args) != 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteLen(0)
			return
		}
		if t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteLen(len(db.hashKeys[key]) * 2)
		for _, k := range db.hashFields(key) {
			c.WriteBulk(k)
			c.WriteBulk(db.hashGet(key, k))
		}
	})
}

// HKEYS
func (m *Miniredis) cmdHkeys(c *server.Peer, cmd string, args []string) {
	if len(args) != 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteLen(0)
			return
		}
		if db.t(key) != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		fields := db.hashFields(key)
		c.WriteLen(len(fields))
		for _, f := range fields {
			c.WriteBulk(f)
		}
	})
}

// HVALS
func (m *Miniredis) cmdHvals(c *server.Peer, cmd string, args []string) {
	if len(args) != 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteLen(0)
			return
		}
		if t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteLen(len(db.hashKeys[key]))
		for _, v := range db.hashKeys[key] {
			c.WriteBulk(v)
		}
	})
}

// HLEN
func (m *Miniredis) cmdHlen(c *server.Peer, cmd string, args []string) {
	if len(args) != 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteInt(0)
			return
		}
		if t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteInt(len(db.hashKeys[key]))
	})
}

// HMGET
func (m *Miniredis) cmdHmget(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		f, ok := db.hashKeys[key]
		if !ok {
			f = map[string]string{}
		}

		c.WriteLen(len(args) - 1)
		for _, k := range args[1:] {
			v, ok := f[k]
			if !ok {
				c.WriteNull()
				continue
			}
			c.WriteBulk(v)
		}
	})
}

// HINCRBY
func (m *Miniredis) cmdHincrby(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, field, deltas := args[0], args[1], args[2]

	delta, err := strconv.Atoi(deltas)
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		v, err := db.hashIncr(key, field, delta)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		c.WriteInt(v)
	})
}

// HINCRBYFLOAT
func (m *Miniredis) cmdHincrbyfloat(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, field, deltas := args[0], args[1], args[2]

	delta, err := strconv.ParseFloat(deltas, 64)
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidFloat)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "hash" {
			c.WriteError(msgWrongType)
			return
		}

		v, err := db.hashIncrfloat(key, field, delta)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		c.WriteBulk(formatFloat(v))
	})
}

// HSCAN
func (m *Miniredis) cmdHscan(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	cursor, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidCursor)
		return
	}
	args = args[2:]

	// MATCH and COUNT options
	var withMatch bool
	var match string
	for len(args) > 0 {
		if strings.ToLower(args[0]) == "count" {
			// we do nothing with count
			if len(args) < 2 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			_, err := strconv.Atoi(args[1])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			args = args[2:]
			continue
		}
		if strings.ToLower(args[0]) == "match" {
			if len(args) < 2 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			withMatch = true
			match, args = args[1], args[2:]
			continue
		}
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		// return _all_ (matched) keys every time

		if cursor != 0 {
			// Invalid cursor.
			c.WriteLen(2)
			c.WriteBulk("0") // no next cursor
			c.WriteLen(0)    // no elements
			return
		}
		if db.exists(key) && db.t(key) != "hash" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.hashFields(key)
		if withMatch {
			members = matchKeys(members, match)
		}

		c.WriteLen(2)
		c.WriteBulk("0") // no next cursor
		// HSCAN gives key, values.
		c.WriteLen(len(members) * 2)
		for _, k := range members {
			c.WriteBulk(k)
			c.WriteBulk(db.hashGet(key, k))
		}
	})
}
