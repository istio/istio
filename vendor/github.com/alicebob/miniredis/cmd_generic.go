// Commands from http://redis.io/commands#generic

package miniredis

import (
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/alicebob/miniredis/server"
)

// commandsGeneric handles EXPIRE, TTL, PERSIST, &c.
func commandsGeneric(m *Miniredis) {
	m.srv.Register("DEL", m.cmdDel)
	// DUMP
	m.srv.Register("EXISTS", m.cmdExists)
	m.srv.Register("EXPIRE", makeCmdExpire(m, false, time.Second))
	m.srv.Register("EXPIREAT", makeCmdExpire(m, true, time.Second))
	m.srv.Register("KEYS", m.cmdKeys)
	// MIGRATE
	m.srv.Register("MOVE", m.cmdMove)
	// OBJECT
	m.srv.Register("PERSIST", m.cmdPersist)
	m.srv.Register("PEXPIRE", makeCmdExpire(m, false, time.Millisecond))
	m.srv.Register("PEXPIREAT", makeCmdExpire(m, true, time.Millisecond))
	m.srv.Register("PTTL", m.cmdPTTL)
	m.srv.Register("RANDOMKEY", m.cmdRandomkey)
	m.srv.Register("RENAME", m.cmdRename)
	m.srv.Register("RENAMENX", m.cmdRenamenx)
	// RESTORE
	// SORT
	m.srv.Register("TTL", m.cmdTTL)
	m.srv.Register("TYPE", m.cmdType)
	m.srv.Register("SCAN", m.cmdScan)
}

// generic expire command for EXPIRE, PEXPIRE, EXPIREAT, PEXPIREAT
// d is the time unit. If unix is set it'll be seen as a unixtimestamp and
// converted to a duration.
func makeCmdExpire(m *Miniredis, unix bool, d time.Duration) func(*server.Peer, string, []string) {
	return func(c *server.Peer, cmd string, args []string) {
		if len(args) != 2 {
			setDirty(c)
			c.WriteError(errWrongNumber(cmd))
			return
		}
		if !m.handleAuth(c) {
			return
		}

		key := args[0]
		value := args[1]
		i, err := strconv.Atoi(value)
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidInt)
			return
		}

		withTx(m, c, func(c *server.Peer, ctx *connCtx) {
			db := m.db(ctx.selectedDB)

			// Key must be present.
			if _, ok := db.keys[key]; !ok {
				c.WriteInt(0)
				return
			}
			if unix {
				var ts time.Time
				switch d {
				case time.Millisecond:
					ts = time.Unix(0, int64(i))
				case time.Second:
					ts = time.Unix(int64(i), 0)
				default:
					panic("invalid time unit (d). Fixme!")
				}
				now := m.now
				if now.IsZero() {
					now = time.Now().UTC()
				}
				db.ttl[key] = ts.Sub(now)
			} else {
				db.ttl[key] = time.Duration(i) * d
			}
			db.keyVersion[key]++
			db.checkTTL(key)
			c.WriteInt(1)
		})
	}
}

// TTL
func (m *Miniredis) cmdTTL(c *server.Peer, cmd string, args []string) {
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

		if _, ok := db.keys[key]; !ok {
			// No such key
			c.WriteInt(-2)
			return
		}

		v, ok := db.ttl[key]
		if !ok {
			// no expire value
			c.WriteInt(-1)
			return
		}
		c.WriteInt(int(v.Seconds()))
	})
}

// PTTL
func (m *Miniredis) cmdPTTL(c *server.Peer, cmd string, args []string) {
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

		if _, ok := db.keys[key]; !ok {
			// no such key
			c.WriteInt(-2)
			return
		}

		v, ok := db.ttl[key]
		if !ok {
			// no expire value
			c.WriteInt(-1)
			return
		}
		c.WriteInt(int(v.Nanoseconds() / 1000000))
	})
}

// PERSIST
func (m *Miniredis) cmdPersist(c *server.Peer, cmd string, args []string) {
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

		if _, ok := db.keys[key]; !ok {
			// no such key
			c.WriteInt(0)
			return
		}

		if _, ok := db.ttl[key]; !ok {
			// no expire value
			c.WriteInt(0)
			return
		}
		delete(db.ttl, key)
		db.keyVersion[key]++
		c.WriteInt(1)
	})
}

// DEL
func (m *Miniredis) cmdDel(c *server.Peer, cmd string, args []string) {
	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		count := 0
		for _, key := range args {
			if db.exists(key) {
				count++
			}
			db.del(key, true) // delete expire
		}
		c.WriteInt(count)
	})
}

// TYPE
func (m *Miniredis) cmdType(c *server.Peer, cmd string, args []string) {
	if len(args) != 1 {
		setDirty(c)
		c.WriteError("usage error")
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
			c.WriteInline("none")
			return
		}

		c.WriteInline(t)
	})
}

// EXISTS
func (m *Miniredis) cmdExists(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		found := 0
		for _, k := range args {
			if db.exists(k) {
				found++
			}
		}
		c.WriteInt(found)
	})
}

// MOVE
func (m *Miniredis) cmdMove(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	targetDB, err := strconv.Atoi(args[1])
	if err != nil {
		targetDB = 0
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		if ctx.selectedDB == targetDB {
			c.WriteError("ERR source and destination objects are the same")
			return
		}
		db := m.db(ctx.selectedDB)
		targetDB := m.db(targetDB)

		if !db.move(key, targetDB) {
			c.WriteInt(0)
			return
		}
		c.WriteInt(1)
	})
}

// KEYS
func (m *Miniredis) cmdKeys(c *server.Peer, cmd string, args []string) {
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

		keys := matchKeys(db.allKeys(), key)
		c.WriteLen(len(keys))
		for _, s := range keys {
			c.WriteBulk(s)
		}
	})
}

// RANDOMKEY
func (m *Miniredis) cmdRandomkey(c *server.Peer, cmd string, args []string) {
	if len(args) != 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if len(db.keys) == 0 {
			c.WriteNull()
			return
		}
		nr := rand.Intn(len(db.keys))
		for k := range db.keys {
			if nr == 0 {
				c.WriteBulk(k)
				return
			}
			nr--
		}
	})
}

// RENAME
func (m *Miniredis) cmdRename(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	from, to := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(from) {
			c.WriteError(msgKeyNotFound)
			return
		}

		db.rename(from, to)
		c.WriteOK()
	})
}

// RENAMENX
func (m *Miniredis) cmdRenamenx(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	from, to := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(from) {
			c.WriteError(msgKeyNotFound)
			return
		}

		if db.exists(to) {
			c.WriteInt(0)
			return
		}

		db.rename(from, to)
		c.WriteInt(1)
	})
}

// SCAN
func (m *Miniredis) cmdScan(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	cursor, err := strconv.Atoi(args[0])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidCursor)
		return
	}
	args = args[1:]

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
			if _, err := strconv.Atoi(args[1]); err != nil {
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
		// We return _all_ (matched) keys every time.

		if cursor != 0 {
			// Invalid cursor.
			c.WriteLen(2)
			c.WriteBulk("0") // no next cursor
			c.WriteLen(0)    // no elements
			return
		}

		keys := db.allKeys()
		if withMatch {
			keys = matchKeys(keys, match)
		}

		c.WriteLen(2)
		c.WriteBulk("0") // no next cursor
		c.WriteLen(len(keys))
		for _, k := range keys {
			c.WriteBulk(k)
		}
	})
}
