package miniredis

import (
	"crypto/sha1"
	"encoding/hex"
	"io"
	"strconv"
	"strings"

	luajson "github.com/alicebob/gopher-json"
	"github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"

	"github.com/alicebob/miniredis/server"
)

func commandsScripting(m *Miniredis) {
	m.srv.Register("EVAL", m.cmdEval)
	m.srv.Register("EVALSHA", m.cmdEvalsha)
	m.srv.Register("SCRIPT", m.cmdScript)
}

func (m *Miniredis) runLuaScript(c *server.Peer, script string, args []string) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()

	// Taken from the go-lua manual
	for _, pair := range []struct {
		n string
		f lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage},
		{lua.BaseLibName, lua.OpenBase},
		{lua.CoroutineLibName, lua.OpenCoroutine},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		if err := l.CallByParam(lua.P{
			Fn:      l.NewFunction(pair.f),
			NRet:    0,
			Protect: true,
		}, lua.LString(pair.n)); err != nil {
			panic(err)
		}
	}

	luajson.Preload(l)
	requireGlobal(l, "cjson", "json")

	conn := m.redigo()
	defer conn.Close()

	// set global variable KEYS
	keysTable := l.NewTable()
	keysS, args := args[0], args[1:]
	keysLen, err := strconv.Atoi(keysS)
	if err != nil {
		c.WriteError(msgInvalidInt)
		return
	}
	if keysLen < 0 {
		c.WriteError(msgNegativeKeysNumber)
		return
	}
	if keysLen > len(args) {
		c.WriteError(msgInvalidKeysNumber)
		return
	}
	keys, args := args[:keysLen], args[keysLen:]
	for i, k := range keys {
		l.RawSet(keysTable, lua.LNumber(i+1), lua.LString(k))
	}
	l.SetGlobal("KEYS", keysTable)

	argvTable := l.NewTable()
	for i, a := range args {
		l.RawSet(argvTable, lua.LNumber(i+1), lua.LString(a))
	}
	l.SetGlobal("ARGV", argvTable)

	redisFuncs := mkLuaFuncs(conn)
	// Register command handlers
	l.Push(l.NewFunction(func(l *lua.LState) int {
		mod := l.RegisterModule("redis", redisFuncs).(*lua.LTable)
		l.Push(mod)
		return 1
	}))

	l.Push(lua.LString("redis"))
	l.Call(1, 0)

	if err := l.DoString(script); err != nil {
		c.WriteError(errLuaParseError(err))
		return
	}

	luaToRedis(l, c, l.Get(1))
}

func (m *Miniredis) cmdEval(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}
	script, args := args[0], args[1:]
	m.runLuaScript(c, script, args)
}

func (m *Miniredis) cmdEvalsha(c *server.Peer, cmd string, args []string) {
	if len(args) < 2 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	sha, args := args[0], args[1:]
	m.Lock()
	script, ok := m.scripts[sha]
	m.Unlock()
	if !ok {
		c.WriteError(msgNoScriptFound)
		return
	}
	m.runLuaScript(c, script, args)
}

func (m *Miniredis) cmdScript(c *server.Peer, cmd string, args []string) {
	if len(args) < 1 {
		setDirty(c)
		c.WriteError(errWrongNumber(cmd))
		return
	}
	if !m.handleAuth(c) {
		return
	}

	subcmd, args := args[0], args[1:]
	switch strings.ToLower(subcmd) {
	case "load":
		if len(args) != 1 {
			setDirty(c)
			c.WriteError(msgScriptUsage)
			return
		}
		script := args[0]
		if _, err := parse.Parse(strings.NewReader(script), "user_script"); err != nil {
			c.WriteError(errLuaParseError(err))
			return
		}
		sha := sha1Hex(script)
		m.Lock()
		m.scripts[sha] = script
		m.Unlock()
		c.WriteBulk(sha)

	case "exists":
		m.Lock()
		defer m.Unlock()
		c.WriteLen(len(args))
		for _, arg := range args {
			if _, ok := m.scripts[arg]; ok {
				c.WriteInt(1)
			} else {
				c.WriteInt(0)
			}
		}

	case "flush":
		if len(args) != 0 {
			setDirty(c)
			c.WriteError(msgScriptUsage)
			return
		}

		m.Lock()
		defer m.Unlock()
		m.scripts = map[string]string{}
		c.WriteOK()

	default:
		c.WriteError(msgScriptUsage)
	}
}

func sha1Hex(s string) string {
	h := sha1.New()
	io.WriteString(h, s)
	return hex.EncodeToString(h.Sum(nil))
}

// requireGlobal imports module modName into the global namespace with the identifier id.  panics if an error results
// from the function execution
func requireGlobal(l *lua.LState, id, modName string) {
	if err := l.CallByParam(lua.P{
		Fn:      l.GetGlobal("require"),
		NRet:    1,
		Protect: true,
	}, lua.LString(modName)); err != nil {
		panic(err)
	}
	mod := l.Get(-1)
	l.Pop(1)

	l.SetGlobal(id, mod)
}
