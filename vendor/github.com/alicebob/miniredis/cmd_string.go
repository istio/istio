// Commands from https://redis.io/commands#string

package miniredis

import (
	"strconv"
	"strings"
	"time"

	"github.com/alicebob/miniredis/server"
)

// commandsString handles all string value operations.
func commandsString(m *Miniredis) {
	m.srv.Register("APPEND", m.cmdAppend)
	m.srv.Register("BITCOUNT", m.cmdBitcount)
	m.srv.Register("BITOP", m.cmdBitop)
	m.srv.Register("BITPOS", m.cmdBitpos)
	m.srv.Register("DECRBY", m.cmdDecrby)
	m.srv.Register("DECR", m.cmdDecr)
	m.srv.Register("GETBIT", m.cmdGetbit)
	m.srv.Register("GET", m.cmdGet)
	m.srv.Register("GETRANGE", m.cmdGetrange)
	m.srv.Register("GETSET", m.cmdGetset)
	m.srv.Register("INCRBYFLOAT", m.cmdIncrbyfloat)
	m.srv.Register("INCRBY", m.cmdIncrby)
	m.srv.Register("INCR", m.cmdIncr)
	m.srv.Register("MGET", m.cmdMget)
	m.srv.Register("MSET", m.cmdMset)
	m.srv.Register("MSETNX", m.cmdMsetnx)
	m.srv.Register("PSETEX", m.cmdPsetex)
	m.srv.Register("SETBIT", m.cmdSetbit)
	m.srv.Register("SETEX", m.cmdSetex)
	m.srv.Register("SET", m.cmdSet)
	m.srv.Register("SETNX", m.cmdSetnx)
	m.srv.Register("SETRANGE", m.cmdSetrange)
	m.srv.Register("STRLEN", m.cmdStrlen)
}

// SET
func (m *Miniredis) cmdSet(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	var (
		nx  = false // set iff not exists
		xx  = false // set iff exists
		ttl time.Duration
	)

	key, value, args := args[0], args[1], args[2:]
	for len(args) > 0 {
		timeUnit := time.Second
		switch strings.ToUpper(args[0]) {
		case "NX":
			nx = true
			args = args[1:]
			continue
		case "XX":
			xx = true
			args = args[1:]
			continue
		case "PX":
			timeUnit = time.Millisecond
			fallthrough
		case "EX":
			if len(args) < 2 {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			expire, err := strconv.Atoi(args[1])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			ttl = time.Duration(expire) * timeUnit
			if ttl <= 0 {
				setDirty(c)
				c.WriteError(msgInvalidSETime)
				return
			}

			args = args[2:]
			continue
		default:
			setDirty(c)
			c.WriteError(msgSyntaxError)
			return
		}
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if nx {
			if db.exists(key) {
				c.WriteNull()
				return
			}
		}
		if xx {
			if !db.exists(key) {
				c.WriteNull()
				return
			}
		}

		db.del(key, true) // be sure to remove existing values of other type keys.
		// a vanilla SET clears the expire
		db.stringSet(key, value)
		if ttl != 0 {
			db.ttl[key] = ttl
		}
		c.WriteOK()
	})
}

// SETEX
func (m *Miniredis) cmdSetex(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	ttl, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	if ttl <= 0 {
		setDirty(c)
		c.WriteError(msgInvalidSETEXTime)
		return
	}
	value := args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		db.del(key, true) // Clear any existing keys.
		db.stringSet(key, value)
		db.ttl[key] = time.Duration(ttl) * time.Second
		c.WriteOK()
	})
}

// PSETEX
func (m *Miniredis) cmdPsetex(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	ttl, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	if ttl <= 0 {
		setDirty(c)
		c.WriteError(msgInvalidPSETEXTime)
		return
	}
	value := args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		db.del(key, true) // Clear any existing keys.
		db.stringSet(key, value)
		db.ttl[key] = time.Duration(ttl) * time.Millisecond
		c.WriteOK()
	})
}

// SETNX
func (m *Miniredis) cmdSetnx(c *server.Peer, cmd string, args []string) {
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

		if _, ok := db.keys[key]; ok {
			c.WriteInt(0)
			return
		}

		db.stringSet(key, value)
		c.WriteInt(1)
	})
}

// MSET
func (m *Miniredis) cmdMset(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	if len(args)%2 != 0 {
		setDirty(c)
		// non-default error message
		c.WriteError("ERR wrong number of arguments for MSET")
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		for len(args) > 0 {
			key, value := args[0], args[1]
			args = args[2:]

			db.del(key, true) // clear TTL
			db.stringSet(key, value)
		}
		c.WriteOK()
	})
}

// MSETNX
func (m *Miniredis) cmdMsetnx(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	if len(args)%2 != 0 {
		setDirty(c)
		// non-default error message (yes, with 'MSET').
		c.WriteError("ERR wrong number of arguments for MSET")
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		keys := map[string]string{}
		existing := false
		for len(args) > 0 {
			key := args[0]
			value := args[1]
			args = args[2:]
			keys[key] = value
			if _, ok := db.keys[key]; ok {
				existing = true
			}
		}

		res := 0
		if !existing {
			res = 1
			for k, v := range keys {
				// Nothing to delete. That's the whole point.
				db.stringSet(k, v)
			}
		}
		c.WriteInt(res)
	})
}

// GET
func (m *Miniredis) cmdGet(c *server.Peer, cmd string, args []string) {
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
			c.WriteNull()
			return
		}
		if db.t(key) != "string" {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteBulk(db.stringGet(key))
	})
}

// GETSET
func (m *Miniredis) cmdGetset(c *server.Peer, cmd string, args []string) {
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

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		old, ok := db.stringKeys[key]
		db.stringSet(key, value)
		// a GETSET clears the ttl
		delete(db.ttl, key)

		if !ok {
			c.WriteNull()
			return
		}
		c.WriteBulk(old)
	})
}

// MGET
func (m *Miniredis) cmdMget(c *server.Peer, cmd string, args []string) {
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

		c.WriteLen(len(args))
		for _, k := range args {
			if t, ok := db.keys[k]; !ok || t != "string" {
				c.WriteNull()
				continue
			}
			v, ok := db.stringKeys[k]
			if !ok {
				// Should not happen, we just checked keys[]
				c.WriteNull()
				continue
			}
			c.WriteBulk(v)
		}
	})
}

// INCR
func (m *Miniredis) cmdIncr(c *server.Peer, cmd string, args []string) {
	if len(args) != 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		key := args[0]
		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}
		v, err := db.stringIncr(key, +1)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		// Don't touch TTL
		c.WriteInt(v)
	})
}

// INCRBY
func (m *Miniredis) cmdIncrby(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	delta, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		v, err := db.stringIncr(key, delta)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		// Don't touch TTL
		c.WriteInt(v)
	})
}

// INCRBYFLOAT
func (m *Miniredis) cmdIncrbyfloat(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	delta, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidFloat)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		v, err := db.stringIncrfloat(key, delta)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		// Don't touch TTL
		c.WriteBulk(formatFloat(v))
	})
}

// DECR
func (m *Miniredis) cmdDecr(c *server.Peer, cmd string, args []string) {
	if len(args) != 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		key := args[0]
		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}
		v, err := db.stringIncr(key, -1)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		// Don't touch TTL
		c.WriteInt(v)
	})
}

// DECRBY
func (m *Miniredis) cmdDecrby(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	delta, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		v, err := db.stringIncr(key, -delta)
		if err != nil {
			c.WriteError(err.Error())
			return
		}
		// Don't touch TTL
		c.WriteInt(v)
	})
}

// STRLEN
func (m *Miniredis) cmdStrlen(c *server.Peer, cmd string, args []string) {
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

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		c.WriteInt(len(db.stringKeys[key]))
	})
}

// APPEND
func (m *Miniredis) cmdAppend(c *server.Peer, cmd string, args []string) {
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

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		newValue := db.stringKeys[key] + value
		db.stringSet(key, newValue)

		c.WriteInt(len(newValue))
	})
}

// GETRANGE
func (m *Miniredis) cmdGetrange(c *server.Peer, cmd string, args []string) {
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

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		v := db.stringKeys[key]
		c.WriteBulk(withRange(v, start, end))
	})
}

// SETRANGE
func (m *Miniredis) cmdSetrange(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	pos, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	if pos < 0 {
		setDirty(c)
		c.WriteError("ERR offset is out of range")
		return
	}
	subst := args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}

		v := []byte(db.stringKeys[key])
		if len(v) < pos+len(subst) {
			newV := make([]byte, pos+len(subst))
			copy(newV, v)
			v = newV
		}
		copy(v[pos:pos+len(subst)], subst)
		db.stringSet(key, string(v))
		c.WriteInt(len(v))
	})
}

// BITCOUNT
func (m *Miniredis) cmdBitcount(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	var (
		useRange   = false
		start, end = 0, 0
		key        = args[0]
	)
	args = args[1:]
	if len(args) >= 2 {
		useRange = true
		var err error
		start, err = strconv.Atoi(args[0])
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidInt)
			return
		}
		end, err = strconv.Atoi(args[1])
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidInt)
			return
		}
		args = args[2:]
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}
		if db.t(key) != "string" {
			c.WriteError(msgWrongType)
			return
		}

		// Real redis only checks after it knows the key is there and a string.
		if len(args) != 0 {
			c.WriteError(msgSyntaxError)
			return
		}

		v := db.stringKeys[key]
		if useRange {
			v = withRange(v, start, end)
		}

		c.WriteInt(countBits([]byte(v)))
	})
}

// BITOP
func (m *Miniredis) cmdBitop(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	var (
		op     = strings.ToUpper(args[0])
		target = args[1]
		input  = args[2:]
	)

	// 'op' is tested when the transaction is executed.
	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		switch op {
		case "AND", "OR", "XOR":
			first := input[0]
			if t, ok := db.keys[first]; ok && t != "string" {
				c.WriteError(msgWrongType)
				return
			}
			res := []byte(db.stringKeys[first])
			for _, vk := range input[1:] {
				if t, ok := db.keys[vk]; ok && t != "string" {
					c.WriteError(msgWrongType)
					return
				}
				v := db.stringKeys[vk]
				cb := map[string]func(byte, byte) byte{
					"AND": func(a, b byte) byte { return a & b },
					"OR":  func(a, b byte) byte { return a | b },
					"XOR": func(a, b byte) byte { return a ^ b },
				}[op]
				res = sliceBinOp(cb, res, []byte(v))
			}
			db.del(target, false) // Keep TTL
			if len(res) == 0 {
				db.del(target, true)
			} else {
				db.stringSet(target, string(res))
			}
			c.WriteInt(len(res))
		case "NOT":
			// NOT only takes a single argument.
			if len(input) != 1 {
				c.WriteError("ERR BITOP NOT must be called with a single source key.")
				return
			}
			key := input[0]
			if t, ok := db.keys[key]; ok && t != "string" {
				c.WriteError(msgWrongType)
				return
			}
			value := []byte(db.stringKeys[key])
			for i := range value {
				value[i] = ^value[i]
			}
			db.del(target, false) // Keep TTL
			if len(value) == 0 {
				db.del(target, true)
			} else {
				db.stringSet(target, string(value))
			}
			c.WriteInt(len(value))
		default:
			c.WriteError(msgSyntaxError)
		}
	})
}

// BITPOS
func (m *Miniredis) cmdBitpos(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 || len(args) > 4 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	bit, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	var start, end int
	withEnd := false
	if len(args) > 2 {
		start, err = strconv.Atoi(args[2])
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidInt)
			return
		}
	}
	if len(args) > 3 {
		end, err = strconv.Atoi(args[3])
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidInt)
			return
		}
		withEnd = true
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}
		value := db.stringKeys[key]
		if start != 0 {
			if start > len(value) {
				start = len(value)
			}
		}
		if withEnd {
			end++ // redis end semantics.
			if end < 0 {
				end = len(value) + end
			}
			if end > len(value) {
				end = len(value)
			}
		} else {
			end = len(value)
		}
		if start != 0 || withEnd {
			if end < start {
				value = ""
			} else {
				value = value[start:end]
			}
		}
		pos := bitPos([]byte(value), bit == 1)
		if pos >= 0 {
			pos += start * 8
		}
		// Special case when looking for 0, but not when start and end are
		// given.
		if bit == 0 && pos == -1 && !withEnd {
			pos = start*8 + len(value)*8
		}
		c.WriteInt(pos)
	})
}

// GETBIT
func (m *Miniredis) cmdGetbit(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	bit, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError("ERR bit offset is not an integer or out of range")
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}
		value := db.stringKeys[key]

		ourByteNr := bit / 8
		var ourByte byte
		if ourByteNr > len(value)-1 {
			ourByte = '\x00'
		} else {
			ourByte = value[ourByteNr]
		}
		res := 0
		if toBits(ourByte)[bit%8] {
			res = 1
		}
		c.WriteInt(res)
	})
}

// SETBIT
func (m *Miniredis) cmdSetbit(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	bit, err := strconv.Atoi(args[1])
	if err != nil || bit < 0 {
		setDirty(c)
		c.WriteError("ERR bit offset is not an integer or out of range")
		return
	}
	newBit, err := strconv.Atoi(args[2])
	if err != nil || (newBit != 0 && newBit != 1) {
		setDirty(c)
		c.WriteError("ERR bit is not an integer or out of range")
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if t, ok := db.keys[key]; ok && t != "string" {
			c.WriteError(msgWrongType)
			return
		}
		value := []byte(db.stringKeys[key])

		ourByteNr := bit / 8
		ourBitNr := bit % 8
		if ourByteNr > len(value)-1 {
			// Too short. Expand.
			newValue := make([]byte, ourByteNr+1)
			copy(newValue, value)
			value = newValue
		}
		old := 0
		if toBits(value[ourByteNr])[ourBitNr] {
			old = 1
		}
		if newBit == 0 {
			value[ourByteNr] &^= 1 << uint8(7-ourBitNr)
		} else {
			value[ourByteNr] |= 1 << uint8(7-ourBitNr)
		}
		db.stringSet(key, string(value))

		c.WriteInt(old)
	})
}

// Redis range. both start and end can be negative.
func withRange(v string, start, end int) string {
	s, e := redisRange(len(v), start, end, true /* string getrange symantics */)
	return v[s:e]
}

func countBits(v []byte) int {
	count := 0
	for _, b := range []byte(v) {
		for b > 0 {
			count += int((b % uint8(2)))
			b = b >> 1
		}
	}
	return count
}

// sliceBinOp applies an operator to all slice elements, with Redis string
// padding logic.
func sliceBinOp(f func(a, b byte) byte, a, b []byte) []byte {
	maxl := len(a)
	if len(b) > maxl {
		maxl = len(b)
	}
	lA := make([]byte, maxl)
	copy(lA, a)
	lB := make([]byte, maxl)
	copy(lB, b)
	res := make([]byte, maxl)
	for i := range res {
		res[i] = f(lA[i], lB[i])
	}
	return res
}

// Return the number of the first bit set/unset.
func bitPos(s []byte, bit bool) int {
	for i, b := range s {
		for j, set := range toBits(b) {
			if set == bit {
				return i*8 + j
			}
		}
	}
	return -1
}

// toBits changes a byte in 8 bools.
func toBits(s byte) [8]bool {
	r := [8]bool{}
	for i := range r {
		if s&(uint8(1)<<uint8(7-i)) != 0 {
			r[i] = true
		}
	}
	return r
}
