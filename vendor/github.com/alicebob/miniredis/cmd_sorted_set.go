// Commands from http://redis.io/commands#sorted_set

package miniredis

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/alicebob/miniredis/server"
)

var (
	errInvalidRangeItem = errors.New(msgInvalidRangeItem)
)

// commandsSortedSet handles all sorted set operations.
func commandsSortedSet(m *Miniredis) {
	m.srv.Register("ZADD", m.cmdZadd)
	m.srv.Register("ZCARD", m.cmdZcard)
	m.srv.Register("ZCOUNT", m.cmdZcount)
	m.srv.Register("ZINCRBY", m.cmdZincrby)
	m.srv.Register("ZINTERSTORE", m.cmdZinterstore)
	m.srv.Register("ZLEXCOUNT", m.cmdZlexcount)
	m.srv.Register("ZRANGE", m.makeCmdZrange(false))
	m.srv.Register("ZRANGEBYLEX", m.cmdZrangebylex)
	m.srv.Register("ZRANGEBYSCORE", m.makeCmdZrangebyscore(false))
	m.srv.Register("ZRANK", m.makeCmdZrank(false))
	m.srv.Register("ZREM", m.cmdZrem)
	m.srv.Register("ZREMRANGEBYLEX", m.cmdZremrangebylex)
	m.srv.Register("ZREMRANGEBYRANK", m.cmdZremrangebyrank)
	m.srv.Register("ZREMRANGEBYSCORE", m.cmdZremrangebyscore)
	m.srv.Register("ZREVRANGE", m.makeCmdZrange(true))
	m.srv.Register("ZREVRANGEBYSCORE", m.makeCmdZrangebyscore(true))
	m.srv.Register("ZREVRANK", m.makeCmdZrank(true))
	m.srv.Register("ZSCORE", m.cmdZscore)
	m.srv.Register("ZUNIONSTORE", m.cmdZunionstore)
	m.srv.Register("ZSCAN", m.cmdZscan)
}

// ZADD
func (m *Miniredis) cmdZadd(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, args := args[0], args[1:]
	var (
		nx    = false
		xx    = false
		ch    = false
		incr  = false
		elems = map[string]float64{}
	)

outer:
	for len(args) > 0 {
		switch strings.ToUpper(args[0]) {
		case "NX":
			nx = true
			args = args[1:]
			continue
		case "XX":
			xx = true
			args = args[1:]
			continue
		case "CH":
			ch = true
			args = args[1:]
			continue
		case "INCR":
			incr = true
			args = args[1:]
			continue
		default:
			break outer
		}
	}

	if len(args) == 0 || len(args)%2 != 0 {
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}
	for len(args) > 0 {
		score, err := strconv.ParseFloat(args[0], 64)
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidFloat)
			return
		}
		elems[args[1]] = score
		args = args[2:]
	}

	if xx && nx {
		setDirty(c)
		c.WriteError(msgXXandNX)
		return
	}

	if incr && len(elems) > 1 {
		setDirty(c)
		c.WriteError(msgSingleElementPair)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if db.exists(key) && db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		if incr {
			for member, delta := range elems {
				if nx && db.ssetExists(key, member) {
					c.WriteNull()
					return
				}
				if xx && !db.ssetExists(key, member) {
					c.WriteNull()
					return
				}
				newScore := db.ssetIncrby(key, member, delta)
				c.WriteBulk(formatFloat(newScore))
			}
			return
		}

		res := 0
		for member, score := range elems {
			if nx && db.ssetExists(key, member) {
				continue
			}
			if xx && !db.ssetExists(key, member) {
				continue
			}
			old := db.ssetScore(key, member)
			if db.ssetAdd(key, score, member) {
				res++
			} else {
				if ch && old != score {
					// if 'CH' is specified, only count changed keys
					res++
				}
			}
		}
		c.WriteInt(res)
	})
}

// ZCARD
func (m *Miniredis) cmdZcard(c *server.Peer, cmd string, args []string) {
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

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		c.WriteInt(db.ssetCard(key))
	})
}

// ZCOUNT
func (m *Miniredis) cmdZcount(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	min, minIncl, err := parseFloatRange(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidMinMax)
		return
	}
	max, maxIncl, err := parseFloatRange(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidMinMax)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.ssetElements(key)
		members = withSSRange(members, min, minIncl, max, maxIncl)
		c.WriteInt(len(members))
	})
}

// ZINCRBY
func (m *Miniredis) cmdZincrby(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
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
	member := args[2]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if db.exists(key) && db.t(key) != "zset" {
			c.WriteError(msgWrongType)
			return
		}
		newScore := db.ssetIncrby(key, member, delta)
		c.WriteBulk(formatFloat(newScore))
	})
}

// ZINTERSTORE
func (m *Miniredis) cmdZinterstore(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	destination := args[0]
	numKeys, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	args = args[2:]
	if len(args) < numKeys {
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}
	if numKeys <= 0 {
		setDirty(c)
		c.WriteError("ERR at least 1 input key is needed for ZUNIONSTORE/ZINTERSTORE")
		return
	}
	keys := args[:numKeys]
	args = args[numKeys:]

	withWeights := false
	weights := []float64{}
	aggregate := "sum"
	for len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "weights":
			if len(args) < numKeys+1 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			for i := 0; i < numKeys; i++ {
				f, err := strconv.ParseFloat(args[i+1], 64)
				if err != nil {
					setDirty(c)
					c.WriteError("ERR weight value is not a float")
					return
				}
				weights = append(weights, f)
			}
			withWeights = true
			args = args[numKeys+1:]
		case "aggregate":
			if len(args) < 2 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			aggregate = strings.ToLower(args[1])
			switch aggregate {
			case "sum", "min", "max":
			default:
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			args = args[2:]
		default:
			setDirty(c)
			c.WriteError(msgSyntaxError)
			return
		}
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		db.del(destination, true)

		// We collect everything and remove all keys which turned out not to be
		// present in every set.
		sset := map[string]float64{}
		counts := map[string]int{}
		for i, key := range keys {
			if !db.exists(key) {
				continue
			}
			if db.t(key) != "zset" {
				c.WriteError(msgWrongType)
				return
			}
			for _, el := range db.ssetElements(key) {
				score := el.score
				if withWeights {
					score *= weights[i]
				}
				counts[el.member]++
				old, ok := sset[el.member]
				if !ok {
					sset[el.member] = score
					continue
				}
				switch aggregate {
				default:
					panic("Invalid aggregate")
				case "sum":
					sset[el.member] += score
				case "min":
					if score < old {
						sset[el.member] = score
					}
				case "max":
					if score > old {
						sset[el.member] = score
					}
				}
			}
		}
		for key, count := range counts {
			if count != numKeys {
				delete(sset, key)
			}
		}
		db.ssetSet(destination, sset)
		c.WriteInt(len(sset))
	})
}

// ZLEXCOUNT
func (m *Miniredis) cmdZlexcount(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	min, minIncl, err := parseLexrange(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}
	max, maxIncl, err := parseLexrange(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.ssetMembers(key)
		// Just key sort. If scores are not the same we don't care.
		sort.Strings(members)
		members = withLexRange(members, min, minIncl, max, maxIncl)

		c.WriteInt(len(members))
	})
}

// ZRANGE and ZREVRANGE
func (m *Miniredis) makeCmdZrange(reverse bool) server.Cmd {
	return func(c *server.Peer, cmd string, args []string) {
		if len(args) < 3 {
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

		withScores := false
		if len(args) > 4 {
			c.WriteError(msgSyntaxError)
			return
		}
		if len(args) == 4 {
			if strings.ToLower(args[3]) != "withscores" {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			withScores = true
		}

		withTx(m, c, func(c *server.Peer, ctx *connCtx) {
			db := m.db(ctx.selectedDB)

			if !db.exists(key) {
				c.WriteLen(0)
				return
			}

			if db.t(key) != "zset" {
				c.WriteError(ErrWrongType.Error())
				return
			}

			members := db.ssetMembers(key)
			if reverse {
				reverseSlice(members)
			}
			rs, re := redisRange(len(members), start, end, false)
			if withScores {
				c.WriteLen((re - rs) * 2)
			} else {
				c.WriteLen(re - rs)
			}
			for _, el := range members[rs:re] {
				c.WriteBulk(el)
				if withScores {
					c.WriteBulk(formatFloat(db.ssetScore(key, el)))
				}
			}
		})
	}
}

// ZRANGEBYLEX
func (m *Miniredis) cmdZrangebylex(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	min, minIncl, err := parseLexrange(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}
	max, maxIncl, err := parseLexrange(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}
	args = args[3:]

	withLimit := false
	limitStart := 0
	limitEnd := 0
	for len(args) > 0 {
		if strings.ToLower(args[0]) == "limit" {
			withLimit = true
			args = args[1:]
			if len(args) < 2 {
				c.WriteError(msgSyntaxError)
				return
			}
			limitStart, err = strconv.Atoi(args[0])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			limitEnd, err = strconv.Atoi(args[1])
			if err != nil {
				setDirty(c)
				c.WriteError(msgInvalidInt)
				return
			}
			args = args[2:]
			continue
		}
		// Syntax error
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteLen(0)
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.ssetMembers(key)
		// Just key sort. If scores are not the same we don't care.
		sort.Strings(members)
		members = withLexRange(members, min, minIncl, max, maxIncl)

		// Apply LIMIT ranges. That's <start> <elements>. Unlike RANGE.
		if withLimit {
			if limitStart < 0 {
				members = nil
			} else {
				if limitStart < len(members) {
					members = members[limitStart:]
				} else {
					// out of range
					members = nil
				}
				if limitEnd >= 0 {
					if len(members) > limitEnd {
						members = members[:limitEnd]
					}
				}
			}
		}

		c.WriteLen(len(members))
		for _, el := range members {
			c.WriteBulk(el)
		}
	})
}

// ZRANGEBYSCORE and ZREVRANGEBYSCORE
func (m *Miniredis) makeCmdZrangebyscore(reverse bool) server.Cmd {
	return func(c *server.Peer, cmd string, args []string) {
		if len(args) < 3 {
			setDirty(c)
			c.WriteError(errWrongNumber(cmd))
			return
		}
		if !m.handleAuth(c) {
			return
		}

		key := args[0]
		min, minIncl, err := parseFloatRange(args[1])
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidMinMax)
			return
		}
		max, maxIncl, err := parseFloatRange(args[2])
		if err != nil {
			setDirty(c)
			c.WriteError(msgInvalidMinMax)
			return
		}
		args = args[3:]

		withScores := false
		withLimit := false
		limitStart := 0
		limitEnd := 0
		for len(args) > 0 {
			if strings.ToLower(args[0]) == "limit" {
				withLimit = true
				args = args[1:]
				if len(args) < 2 {
					c.WriteError(msgSyntaxError)
					return
				}
				limitStart, err = strconv.Atoi(args[0])
				if err != nil {
					setDirty(c)
					c.WriteError(msgInvalidInt)
					return
				}
				limitEnd, err = strconv.Atoi(args[1])
				if err != nil {
					setDirty(c)
					c.WriteError(msgInvalidInt)
					return
				}
				args = args[2:]
				continue
			}
			if strings.ToLower(args[0]) == "withscores" {
				withScores = true
				args = args[1:]
				continue
			}
			setDirty(c)
			c.WriteError(msgSyntaxError)
			return
		}

		withTx(m, c, func(c *server.Peer, ctx *connCtx) {
			db := m.db(ctx.selectedDB)

			if !db.exists(key) {
				c.WriteLen(0)
				return
			}

			if db.t(key) != "zset" {
				c.WriteError(ErrWrongType.Error())
				return
			}

			members := db.ssetElements(key)
			if reverse {
				min, max = max, min
				minIncl, maxIncl = maxIncl, minIncl
			}
			members = withSSRange(members, min, minIncl, max, maxIncl)
			if reverse {
				reverseElems(members)
			}

			// Apply LIMIT ranges. That's <start> <elements>. Unlike RANGE.
			if withLimit {
				if limitStart < 0 {
					members = ssElems{}
				} else {
					if limitStart < len(members) {
						members = members[limitStart:]
					} else {
						// out of range
						members = ssElems{}
					}
					if limitEnd >= 0 {
						if len(members) > limitEnd {
							members = members[:limitEnd]
						}
					}
				}
			}

			if withScores {
				c.WriteLen(len(members) * 2)
			} else {
				c.WriteLen(len(members))
			}
			for _, el := range members {
				c.WriteBulk(el.member)
				if withScores {
					c.WriteBulk(formatFloat(el.score))
				}
			}
		})
	}
}

// ZRANK and ZREVRANK
func (m *Miniredis) makeCmdZrank(reverse bool) server.Cmd {
	return func(c *server.Peer, cmd string, args []string) {
		if len(args) != 2 {
			setDirty(c)
			c.WriteError(errWrongNumber(cmd))
			return
		}
		if !m.handleAuth(c) {
			return
		}

		key, member := args[0], args[1]

		withTx(m, c, func(c *server.Peer, ctx *connCtx) {
			db := m.db(ctx.selectedDB)

			if !db.exists(key) {
				c.WriteNull()
				return
			}

			if db.t(key) != "zset" {
				c.WriteError(ErrWrongType.Error())
				return
			}

			direction := asc
			if reverse {
				direction = desc
			}
			rank, ok := db.ssetRank(key, member, direction)
			if !ok {
				c.WriteNull()
				return
			}
			c.WriteInt(rank)
		})
	}
}

// ZREM
func (m *Miniredis) cmdZrem(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, members := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		deleted := 0
		for _, member := range members {
			if db.ssetRem(key, member) {
				deleted++
			}
		}
		c.WriteInt(deleted)
	})
}

// ZREMRANGEBYLEX
func (m *Miniredis) cmdZremrangebylex(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	min, minIncl, err := parseLexrange(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}
	max, maxIncl, err := parseLexrange(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(err.Error())
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.ssetMembers(key)
		// Just key sort. If scores are not the same we don't care.
		sort.Strings(members)
		members = withLexRange(members, min, minIncl, max, maxIncl)

		for _, el := range members {
			db.ssetRem(key, el)
		}
		c.WriteInt(len(members))
	})
}

// ZREMRANGEBYRANK
func (m *Miniredis) cmdZremrangebyrank(c *server.Peer, cmd string, args []string) {
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

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.ssetMembers(key)
		rs, re := redisRange(len(members), start, end, false)
		for _, el := range members[rs:re] {
			db.ssetRem(key, el)
		}
		c.WriteInt(re - rs)
	})
}

// ZREMRANGEBYSCORE
func (m *Miniredis) cmdZremrangebyscore(c *server.Peer, cmd string, args []string) {
	if len(args) != 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key := args[0]
	min, minIncl, err := parseFloatRange(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidMinMax)
		return
	}
	max, maxIncl, err := parseFloatRange(args[2])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidMinMax)
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteInt(0)
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.ssetElements(key)
		members = withSSRange(members, min, minIncl, max, maxIncl)

		for _, el := range members {
			db.ssetRem(key, el.member)
		}
		c.WriteInt(len(members))
	})
}

// ZSCORE
func (m *Miniredis) cmdZscore(c *server.Peer, cmd string, args []string) {
	if len(args) != 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	key, member := args[0], args[1]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		if !db.exists(key) {
			c.WriteNull()
			return
		}

		if db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		if !db.ssetExists(key, member) {
			c.WriteNull()
			return
		}

		c.WriteBulk(formatFloat(db.ssetScore(key, member)))
	})
}

// parseFloatRange handles ZRANGEBYSCORE floats. They are inclusive unless the
// string starts with '('
func parseFloatRange(s string) (float64, bool, error) {
	if len(s) == 0 {
		return 0, false, nil
	}
	inclusive := true
	if s[0] == '(' {
		s = s[1:]
		inclusive = false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, inclusive, err
}

// parseLexrange handles ZRANGEBYLEX ranges. They start with '[', '(', or are
// '+' or '-'.
// Returns range, inclusive, error.
// On '+' or '-' that's just returned.
func parseLexrange(s string) (string, bool, error) {
	if len(s) == 0 {
		return "", false, errInvalidRangeItem
	}
	if s == "+" || s == "-" {
		return s, false, nil
	}
	switch s[0] {
	case '(':
		return s[1:], false, nil
	case '[':
		return s[1:], true, nil
	default:
		return "", false, errInvalidRangeItem
	}
}

// withSSRange limits a list of sorted set elements by the ZRANGEBYSCORE range
// logic.
func withSSRange(members ssElems, min float64, minIncl bool, max float64, maxIncl bool) ssElems {
	gt := func(a, b float64) bool { return a > b }
	gteq := func(a, b float64) bool { return a >= b }

	mincmp := gt
	if minIncl {
		mincmp = gteq
	}
	for i, m := range members {
		if mincmp(m.score, min) {
			members = members[i:]
			goto checkmax
		}
	}
	// all elements were smaller
	return nil

checkmax:
	maxcmp := gteq
	if maxIncl {
		maxcmp = gt
	}
	for i, m := range members {
		if maxcmp(m.score, max) {
			members = members[:i]
			break
		}
	}

	return members
}

// withLexRange limits a list of sorted set elements.
func withLexRange(members []string, min string, minIncl bool, max string, maxIncl bool) []string {
	if max == "-" || min == "+" {
		return nil
	}
	if min != "-" {
		if minIncl {
			for i, m := range members {
				if m >= min {
					members = members[i:]
					break
				}
			}
		} else {
			// Excluding min
			for i, m := range members {
				if m > min {
					members = members[i:]
					break
				}
			}
		}
	}
	if max != "+" {
		if maxIncl {
			for i, m := range members {
				if m > max {
					members = members[:i]
					break
				}
			}
		} else {
			// Excluding max
			for i, m := range members {
				if m >= max {
					members = members[:i]
					break
				}
			}
		}
	}
	return members
}

// ZUNIONSTORE
func (m *Miniredis) cmdZunionstore(c *server.Peer, cmd string, args []string) {
	if len(args) < 3 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	destination := args[0]
	numKeys, err := strconv.Atoi(args[1])
	if err != nil {
		setDirty(c)
		c.WriteError(msgInvalidInt)
		return
	}
	args = args[2:]
	if len(args) < numKeys {
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}
	if numKeys <= 0 {
		setDirty(c)
		c.WriteError("ERR at least 1 input key is needed for ZUNIONSTORE/ZINTERSTORE")
		return
	}
	keys := args[:numKeys]
	args = args[numKeys:]

	withWeights := false
	weights := []float64{}
	aggregate := "sum"
	for len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "weights":
			if len(args) < numKeys+1 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			for i := 0; i < numKeys; i++ {
				f, err := strconv.ParseFloat(args[i+1], 64)
				if err != nil {
					setDirty(c)
					c.WriteError("ERR weight value is not a float")
					return
				}
				weights = append(weights, f)
			}
			withWeights = true
			args = args[numKeys+1:]
		case "aggregate":
			if len(args) < 2 {
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			}
			aggregate = strings.ToLower(args[1])
			switch aggregate {
			default:
				setDirty(c)
				c.WriteError(msgSyntaxError)
				return
			case "sum", "min", "max":
			}
			args = args[2:]
		default:
			setDirty(c)
			c.WriteError(msgSyntaxError)
			return
		}
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)
		deleteDest := true
		for _, key := range keys {
			if destination == key {
				deleteDest = false
			}
		}
		if deleteDest {
			db.del(destination, true)
		}

		sset := sortedSet{}
		for i, key := range keys {
			if !db.exists(key) {
				continue
			}
			if db.t(key) != "zset" {
				c.WriteError(msgWrongType)
				return
			}
			for _, el := range db.ssetElements(key) {
				score := el.score
				if withWeights {
					score *= weights[i]
				}
				old, ok := sset[el.member]
				if !ok {
					sset[el.member] = score
					continue
				}
				switch aggregate {
				default:
					panic("Invalid aggregate")
				case "sum":
					sset[el.member] += score
				case "min":
					if score < old {
						sset[el.member] = score
					}
				case "max":
					if score > old {
						sset[el.member] = score
					}
				}
			}
		}
		db.ssetSet(destination, sset)
		c.WriteInt(sset.card())
	})
}

// ZSCAN
func (m *Miniredis) cmdZscan(c *server.Peer, cmd string, args []string) {
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
		// We return _all_ (matched) keys every time.

		if cursor != 0 {
			// Invalid cursor.
			c.WriteLen(2)
			c.WriteBulk("0") // no next cursor
			c.WriteLen(0)    // no elements
			return
		}
		if db.exists(key) && db.t(key) != "zset" {
			c.WriteError(ErrWrongType.Error())
			return
		}

		members := db.ssetMembers(key)
		if withMatch {
			members = matchKeys(members, match)
		}

		c.WriteLen(2)
		c.WriteBulk("0") // no next cursor
		// HSCAN gives key, values.
		c.WriteLen(len(members) * 2)
		for _, k := range members {
			c.WriteBulk(k)
			c.WriteBulk(formatFloat(db.ssetScore(key, k)))
		}
	})
}
