// Commands from https://redis.io/commands#list

package miniredis

import (
	"strconv"
	"strings"
	"time"

	"github.com/alicebob/miniredis/server"
)

type leftright int

const (
	left leftright = iota
	right
)

// commandsList handles list commands (mostly L*)
func commandsList(m *Miniredis) {
	m.srv.Register("BLPOP", m.cmdBlpop)
	m.srv.Register("BRPOP", m.cmdBrpop)
	m.srv.Register("BRPOPLPUSH", m.cmdBrpoplpush)
	m.srv.Register("LINDEX", m.cmdLindex)
	m.srv.Register("LINSERT", m.cmdLinsert)
	m.srv.Register("LLEN", m.cmdLlen)
	m.srv.Register("LPOP", m.cmdLpop)
	m.srv.Register("LPUSH", m.cmdLpush)
	m.srv.Register("LPUSHX", m.cmdLpushx)
	m.srv.Register("LRANGE", m.cmdLrange)
	m.srv.Register("LREM", m.cmdLrem)
	m.srv.Register("LSET", m.cmdLset)
	m.srv.Register("LTRIM", m.cmdLtrim)
	m.srv.Register("RPOP", m.cmdRpop)
	m.srv.Register("RPOPLPUSH", m.cmdRpoplpush)
	m.srv.Register("RPUSH", m.cmdRpush)
	m.srv.Register("RPUSHX", m.cmdRpushx)
}

// BLPOP
func (m *Miniredis) cmdBlpop(c *server.Peer, cmd string, args []string) {
	m.cmdBXpop(c, cmd, args, left)
}

// BRPOP
func (m *Miniredis) cmdBrpop(c *server.Peer, cmd string, args []string) {
	m.cmdBXpop(c, cmd, args, right)
}

func (m *Miniredis) cmdBXpop(c *server.Peer, cmd string, args []string, lr leftright) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}
	timeoutS := args[len(args)-1]
	keys := args[:len(args)-1]

	timeout, err := strconv.Atoi(timeoutS)
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidTimeout)
		return
	}
	if timeout < 0 {
		setDirty(c)
		c.WriteError(msgNegTimeout)
		return
	}

	blocking(
		m,
		c,
		time.Duration(timeout)*time.Second,
		func(c *server.Peer, ctx *connCtx) bool {
			db := m.db(ctx.selectedDB)
			for _, key := range keys {
				if !db.exists(key) {
					continue
				}
				if db.t(key) != "list" {
					c.WriteError(msgWrongType)
					return true
				}

				if len(db.listKeys[key]) == 0 {
					continue
				}
				c.WriteLen(2)
				c.WriteBulk(key)
				var v string
				switch lr {
				case left:
					v = db.listLpop(key)
				case right:
					v = db.listPop(key)
				}
				c.WriteBulk(v)
				return true
			}
			return false
		},
		func(c *server.Peer) {
			// timeout
			c.WriteNull()
		},
	)
}

// LINDEX
func (m *Miniredis) cmdLindex(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, offsets := args[0], args[1]

	offset, err := strconv.Atoi(offsets)
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			// No such key
			c.WriteNull()
			return
		}
		if t != "list" {
			c.WriteError(msgWrongType)
			return
		}

		l := db.listKeys[key]
		if offset < 0 {
			offset = len(l) + offset
		}
		if offset < 0 || offset > len(l)-1 {
			c.WriteNull()
			return
		}
		c.WriteBulk(l[offset])
	})
}

// LINSERT
func (m *Miniredis) cmdLinsert(c *server.Peer, cmd string, args []string) {
	if len(args) != 4 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	where := 0
	switch strings.ToLower(args[1]) {
	case "before":
		where = -1
	case "after":
		where = +1
	default:
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}
	pivot := args[2]
	value := args[3]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			// No such key
			c.WriteInt(0)
			return
		}
		if t != "list" {
			c.WriteError(msgWrongType)
			return
		}

		l := db.listKeys[key]
		for i, el := range l {
			if el != pivot {
				continue
			}

			if where < 0 {
				l = append(l[:i], append(listKey{value}, l[i:]...)...)
			} else {
				if i == len(l)-1 {
					l = append(l, value)
				} else {
					l = append(l[:i+1], append(listKey{value}, l[i+1:]...)...)
				}
			}
			db.listKeys[key] = l
			db.keyVersion[key]++
			c.WriteInt(len(l))
			return
		}
		c.WriteInt(-1)
	})
}

// LLEN
func (m *Miniredis) cmdLlen(c *server.Peer, cmd string, args []string) {
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
			// No such key. That's zero length.
			c.WriteInt(0)
			return
		}
		if t != "list" {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteInt(len(db.listKeys[key]))
	})
}

// LPOP
func (m *Miniredis) cmdLpop(c *server.Peer, cmd string, args []string) {
	m.cmdXpop(c, cmd, args, left)
}

// RPOP
func (m *Miniredis) cmdRpop(c *server.Peer, cmd string, args []string) {
	m.cmdXpop(c, cmd, args, right)
}

func (m *Miniredis) cmdXpop(c *server.Peer, cmd string, args []string, lr leftright) {
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
			// non-existing key is fine
			c.WriteNull()
			return
		}
		if db.t(key) != "list" {
			c.WriteError(msgWrongType)
			return
		}

		var elem string
		switch lr {
		case left:
			elem = db.listLpop(key)
		case right:
			elem = db.listPop(key)
		}
		c.WriteBulk(elem)
	})
}

// LPUSH
func (m *Miniredis) cmdLpush(c *server.Peer, cmd string, args []string) {
	m.cmdXpush(c, cmd, args, left)
}

// RPUSH
func (m *Miniredis) cmdRpush(c *server.Peer, cmd string, args []string) {
	m.cmdXpush(c, cmd, args, right)
}

func (m *Miniredis) cmdXpush(c *server.Peer, cmd string, args []string, lr leftright) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, args := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if db.exists(key) && db.t(key) != "list" {
			c.WriteError(msgWrongType)
			return
		}

		var newLen int
		for _, value := range args {
			switch lr {
			case left:
				newLen = db.listLpush(key, value)
			case right:
				newLen = db.listPush(key, value)
			}
		}
		c.WriteInt(newLen)
	})
}

// LPUSHX
func (m *Miniredis) cmdLpushx(c *server.Peer, cmd string, args []string) {
	m.cmdXpushx(c, cmd, args, left)
}

// RPUSHX
func (m *Miniredis) cmdRpushx(c *server.Peer, cmd string, args []string) {
	m.cmdXpushx(c, cmd, args, right)
}

func (m *Miniredis) cmdXpushx(c *server.Peer, cmd string, args []string, lr leftright) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, args := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}
		if db.t(key) != "list" {
			c.WriteError(msgWrongType)
			return
		}

		var newLen int
		for _, value := range args {
			switch lr {
			case left:
				newLen = db.listLpush(key, value)
			case right:
				newLen = db.listPush(key, value)
			}
		}
		c.WriteInt(newLen)
	})
}

// LRANGE
func (m *Miniredis) cmdLrange(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	start, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	end, err := strconv.Atoi(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "list" {
			c.WriteError(msgWrongType)
			return
		}

		l := db.listKeys[key]
		if len(l) == 0 {
			c.WriteLen(0)
			return
		}

		rs, re := redisRange(len(l), start, end, false)
		c.WriteLen(re - rs)
		for _, el := range l[rs:re] {
			c.WriteBulk(el)
		}
	})
}

// LREM
func (m *Miniredis) cmdLrem(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	count, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	value := args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}
		if db.t(key) != "list" {
			c.WriteError(msgWrongType)
			return
		}

		l := db.listKeys[key]
		if count < 0 {
			reverseSlice(l)
		}
		deleted := 0
		newL := []string{}
		toDelete := len(l)
		if count < 0 {
			toDelete = -count
		}
		if count > 0 {
			toDelete = count
		}
		for _, el := range l {
			if el == value {
				if toDelete > 0 {
					deleted++
					toDelete--
					continue
				}
			}
			newL = append(newL, el)
		}
		if count < 0 {
			reverseSlice(newL)
		}
		if len(newL) == 0 {
			db.del(key, true)
		} else {
			db.listKeys[key] = newL
			db.keyVersion[key]++
		}

		c.WriteInt(deleted)
	})
}

// LSET
func (m *Miniredis) cmdLset(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	index, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	value := args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteError(msgKeyNotFound)
			return
		}
		if db.t(key) != "list" {
			c.WriteError(msgWrongType)
			return
		}

		l := db.listKeys[key]
		if index < 0 {
			index = len(l) + index
		}
		if index < 0 || index > len(l)-1 {
			c.WriteError(msgOutOfRange)
			return
		}
		l[index] = value
		db.keyVersion[key]++

		c.WriteOK()
	})
}

// LTRIM
func (m *Miniredis) cmdLtrim(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	start, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	end, err := strconv.Atoi(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		t, ok := db.keys[key]
		if !ok {
			c.WriteOK()
			return
		}
		if t != "list" {
			c.WriteError(msgWrongType)
			return
		}

		l := db.listKeys[key]
		rs, re := redisRange(len(l), start, end, false)
		l = l[rs:re]
		if len(l) == 0 {
			db.del(key, true)
		} else {
			db.listKeys[key] = l
			db.keyVersion[key]++
		}
		c.WriteOK()
	})
}

// RPOPLPUSH
func (m *Miniredis) cmdRpoplpush(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	src, dst := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(src) {
			c.WriteNull()
			return
		}
		if db.t(src) != "list" || (db.exists(dst) && db.t(dst) != "list") {
			c.WriteError(msgWrongType)
			return
		}
		elem := db.listPop(src)
		db.listLpush(dst, elem)
		c.WriteBulk(elem)
	})
}

// BRPOPLPUSH
func (m *Miniredis) cmdBrpoplpush(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	src := args[0]
	dst := args[1]
	timeout, err := strconv.Atoi(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidTimeout)
		return
	}
	if timeout < 0 {
		setDirty(c)
		c.WriteError(msgNegTimeout)
		return
	}

	blocking(
		m,
		c,
		time.Duration(timeout)*time.Second,
		func(c *server.Peer, ctx *connCtx) bool {
			db := m.db(ctx.selectedDB)

			if !db.exists(src) {
				return false
			}
			if db.t(src) != "list" || (db.exists(dst) && db.t(dst) != "list") {
				c.WriteError(msgWrongType)
				return true
			}
			if len(db.listKeys[src]) == 0 {
				return false
			}
			elem := db.listPop(src)
			db.listLpush(dst, elem)
			c.WriteBulk(elem)
			return true
		},
		func(c *server.Peer) {
			// timeout
			c.WriteNull()
		},
	)
}
