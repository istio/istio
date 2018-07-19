// Commands from http://redis.io/commands#set

package miniredis

import (
	"math/rand"
	"strconv"
	"strings"

	"github.com/alicebob/miniredis/server"
)

// commandsSet handles all set value operations.
func commandsSet(m *Miniredis) {
	m.srv.Register("SADD", m.cmdSadd)
	m.srv.Register("SCARD", m.cmdScard)
	m.srv.Register("SDIFF", m.cmdSdiff)
	m.srv.Register("SDIFFSTORE", m.cmdSdiffstore)
	m.srv.Register("SINTER", m.cmdSinter)
	m.srv.Register("SINTERSTORE", m.cmdSinterstore)
	m.srv.Register("SISMEMBER", m.cmdSismember)
	m.srv.Register("SMEMBERS", m.cmdSmembers)
	m.srv.Register("SMOVE", m.cmdSmove)
	m.srv.Register("SPOP", m.cmdSpop)
	m.srv.Register("SRANDMEMBER", m.cmdSrandmember)
	m.srv.Register("SREM", m.cmdSrem)
	m.srv.Register("SUNION", m.cmdSunion)
	m.srv.Register("SUNIONSTORE", m.cmdSunionstore)
	m.srv.Register("SSCAN", m.cmdSscan)
}

// SADD
func (m *Miniredis) cmdSadd(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, elems := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if db.exists(key) && db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		added := db.setAdd(key, elems...)
		c.WriteInt(added)
	})
}

// SCARD
func (m *Miniredis) cmdScard(c *server.Peer, cmd string, args []string) {
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
			c.WriteInt(0)
			return
		}

		if db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.setMembers(key)
		c.WriteInt(len(members))
	})
}

// SDIFF
func (m *Miniredis) cmdSdiff(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	keys := args

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		set, err := db.setDiff(keys)
		if err != nil {
			c.WriteError(err.Error())
			return
		}

		c.WriteLen(len(set))
		for k := range set {
			c.WriteBulk(k)
		}
	})
}

// SDIFFSTORE
func (m *Miniredis) cmdSdiffstore(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	dest, keys := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		set, err := db.setDiff(keys)
		if err != nil {
			c.WriteError(err.Error())
			return
		}

		db.del(dest, true)
		db.setSet(dest, set)
		c.WriteInt(len(set))
	})
}

// SINTER
func (m *Miniredis) cmdSinter(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	keys := args

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		set, err := db.setInter(keys)
		if err != nil {
			c.WriteError(err.Error())
			return
		}

		c.WriteLen(len(set))
		for k := range set {
			c.WriteBulk(k)
		}
	})
}

// SINTERSTORE
func (m *Miniredis) cmdSinterstore(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	dest, keys := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		set, err := db.setInter(keys)
		if err != nil {
			c.WriteError(err.Error())
			return
		}

		db.del(dest, true)
		db.setSet(dest, set)
		c.WriteInt(len(set))
	})
}

// SISMEMBER
func (m *Miniredis) cmdSismember(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, value := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		if db.setIsMember(key, value) {
			c.WriteInt(1)
			return
		}
		c.WriteInt(0)
	})
}

// SMEMBERS
func (m *Miniredis) cmdSmembers(c *server.Peer, cmd string, args []string) {
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

		if db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.setMembers(key)

		c.WriteLen(len(members))
		for _, elem := range members {
			c.WriteBulk(elem)
		}
	})
}

// SMOVE
func (m *Miniredis) cmdSmove(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	src, dst, member := args[0], args[1], args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(src) {
			c.WriteInt(0)
			return
		}

		if db.t(src) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		if db.exists(dst) && db.t(dst) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		if !db.setIsMember(src, member) {
			c.WriteInt(0)
			return
		}
		db.setRem(src, member)
		db.setAdd(dst, member)
		c.WriteInt(1)
	})
}

// SPOP
func (m *Miniredis) cmdSpop(c *server.Peer, cmd string, args []string) {
	if len(args) == 0 {
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

		withCount := false
		count := 1
		if len(args) > 0 {
			v, err := strconv.Atoi(args[0])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			count = v
			withCount = true
			args = args[1:]
		}
		if len(args) > 0 {
			setDirty(c)
			c.WriteError(msgInvalidInt)
			return
		}

		if !db.exists(key) {
			if !withCount {
				c.WriteNull()
				return
			}
			c.WriteLen(0)
			return
		}

		if db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		var deleted []string
		for i := 0; i < count; i++ {
			members := db.setMembers(key)
			if len(members) == 0 {
				break
			}
			member := members[rand.Intn(len(members))]
			db.setRem(key, member)
			deleted = append(deleted, member)
		}
		// without `count` return a single value...
		if !withCount {
			if len(deleted) == 0 {
				c.WriteNull()
				return
			}
			c.WriteBulk(deleted[0])
			return
		}
		// ... with `count` return a list
		c.WriteLen(len(deleted))
		for _, v := range deleted {
			c.WriteBulk(v)
		}
	})
}

// SRANDMEMBER
func (m *Miniredis) cmdSrandmember(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if len(args) > 2 {
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	count := 0
	withCount := false
	if len(args) == 2 {
		var err error
		count, err = strconv.Atoi(args[1])
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidInt)
			return
		}
		withCount = true
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteNull()
			return
		}

		if db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.setMembers(key)
		if count < 0 {
			// Non-unique elements is allowed with negative count.
			c.WriteLen(-count)
			for count != 0 {
				member := members[rand.Intn(len(members))]
				c.WriteBulk(member)
				count++
			}
			return
		}

		// Must be unique elements.
		shuffle(members)
		if count > len(members) {
			count = len(members)
		}
		if !withCount {
			c.WriteBulk(members[0])
			return
		}
		c.WriteLen(count)
		for i := range make([]struct{}, count) {
			c.WriteBulk(members[i])
		}
	})
}

// SREM
func (m *Miniredis) cmdSrem(c *server.Peer, cmd string, args []string) {
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

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		c.WriteInt(db.setRem(key, fields...))
	})
}

// SUNION
func (m *Miniredis) cmdSunion(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	keys := args

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		set, err := db.setUnion(keys)
		if err != nil {
			c.WriteError(err.Error())
			return
		}

		c.WriteLen(len(set))
		for k := range set {
			c.WriteBulk(k)
		}
	})
}

// SUNIONSTORE
func (m *Miniredis) cmdSunionstore(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	dest, keys := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		set, err := db.setUnion(keys)
		if err != nil {
			c.WriteError(err.Error())
			return
		}

		db.del(dest, true)
		db.setSet(dest, set)
		c.WriteInt(len(set))
	})
}

// SSCAN
func (m *Miniredis) cmdSscan(c *server.Peer, cmd string, args []string) {
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
			// We do nothing with count.
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
			match = args[1]
			args = args[2:]
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
			// invalid cursor
			c.WriteLen(2)
			c.WriteBulk("0") // no next cursor
			c.WriteLen(0)    // no elements
			return
		}
		if db.exists(key) && db.t(key) != "set" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.setMembers(key)
		if withMatch {
			members = matchKeys(members, match)
		}

		c.WriteLen(2)
		c.WriteBulk("0") // no next cursor
		c.WriteLen(len(members))
		for _, k := range members {
			c.WriteBulk(k)
		}
	})
}

// shuffle shuffles a string. Kinda.
func shuffle(m []string) {
	for _ = range m {
		i := rand.Intn(len(m))
		j := rand.Intn(len(m))
		m[i], m[j] = m[j], m[i]
	}
}
