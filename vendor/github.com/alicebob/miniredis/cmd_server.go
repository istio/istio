// Commands from http://redis.io/commands#server

package miniredis

import (
	"strings"

	"github.com/alicebob/miniredis/server"
)

func commandsServer(m *Miniredis) {
	m.srv.Register("DBSIZE", m.cmdDbsize)
	m.srv.Register("FLUSHALL", m.cmdFlushall)
	m.srv.Register("FLUSHDB", m.cmdFlushdb)
}

// DBSIZE
func (m *Miniredis) cmdDbsize(c *server.Peer, cmd string, args []string) {
	if len(args) > 0 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		db := m.db(ctx.selectedDB)

		c.WriteInt(len(db.keys))
	})
}

// FLUSHALL
func (m *Miniredis) cmdFlushall(c *server.Peer, cmd string, args []string) {
	if len(args) > 0 && strings.ToLower(args[0]) == "async" {
		args = args[1:]
	}
	if len(args) > 0 {
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}

	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		m.flushAll()
		c.WriteOK()
	})
}

// FLUSHDB
func (m *Miniredis) cmdFlushdb(c *server.Peer, cmd string, args []string) {
	if len(args) > 0 && strings.ToLower(args[0]) == "async" {
		args = args[1:]
	}
	if len(args) > 0 {
		setDirty(c)
		c.WriteError(msgSyntaxError)
		return
	}

	if !m.handleAuth(c) {
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		m.db(ctx.selectedDB).flush()
		c.WriteOK()
	})
}
