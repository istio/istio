package server

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"unicode"
)

func errUnknownCommand(cmd string) string {
	return fmt.Sprintf("ERR unknown command '%s'", strings.ToLower(cmd))
}

// Cmd is what Register expects
type Cmd func(c *Peer, cmd string, args []string)

// Server is a simple redis server
type Server struct {
	l         net.Listener
	cmds      map[string]Cmd
	peers     map[net.Conn]struct{}
	mu        sync.Mutex
	wg        sync.WaitGroup
	infoConns int
	infoCmds  int
}

// NewServer makes a server listening on addr. Close with .Close().
func NewServer(addr string) (*Server, error) {
	s := Server{
		cmds:  map[string]Cmd{},
		peers: map[net.Conn]struct{}{},
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s.l = l

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.serve(l)
	}()
	return &s, nil
}

func (s *Server) serve(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		s.ServeConn(conn)
	}
}

// ServeConn handles a net.Conn. Nice with net.Pipe()
func (s *Server) ServeConn(conn net.Conn) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer conn.Close()
		s.mu.Lock()
		s.peers[conn] = struct{}{}
		s.infoConns++
		s.mu.Unlock()

		s.servePeer(conn)

		s.mu.Lock()
		delete(s.peers, conn)
		s.mu.Unlock()
	}()
}

// Addr has the net.Addr struct
func (s *Server) Addr() *net.TCPAddr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.l == nil {
		return nil
	}
	return s.l.Addr().(*net.TCPAddr)
}

// Close a server started with NewServer. It will wait until all clients are
// closed.
func (s *Server) Close() {
	s.mu.Lock()
	if s.l != nil {
		s.l.Close()
	}
	s.l = nil
	for c := range s.peers {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// Register a command. It can't have been registered before. Safe to call on a
// running server.
func (s *Server) Register(cmd string, f Cmd) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd = strings.ToUpper(cmd)
	if _, ok := s.cmds[cmd]; ok {
		return fmt.Errorf("command already registered: %s", cmd)
	}
	s.cmds[cmd] = f
	return nil
}

func (s *Server) servePeer(c net.Conn) {
	r := bufio.NewReader(c)
	cl := &Peer{
		w: bufio.NewWriter(c),
	}
	for {
		args, err := readArray(r)
		if err != nil {
			return
		}
		s.dispatch(cl, args)
		cl.w.Flush()
		if cl.closed {
			c.Close()
			return
		}
	}
}

func (s *Server) dispatch(c *Peer, args []string) {
	cmd, args := strings.ToUpper(args[0]), args[1:]
	s.mu.Lock()
	cb, ok := s.cmds[cmd]
	s.mu.Unlock()
	if !ok {
		c.WriteError(errUnknownCommand(cmd))
		return
	}

	s.mu.Lock()
	s.infoCmds++
	s.mu.Unlock()
	cb(c, cmd, args)
}

// TotalCommands is total (known) commands since this the server started
func (s *Server) TotalCommands() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.infoCmds
}

// ClientsLen gives the number of connected clients right now
func (s *Server) ClientsLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.peers)
}

// TotalConnections give the number of clients connected since the server
// started, including the currently connected ones
func (s *Server) TotalConnections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.infoConns
}

// Peer is a client connected to the server
type Peer struct {
	w      *bufio.Writer
	closed bool
	Ctx    interface{} // anything goes, server won't touch this
}

// Flush the write buffer. Called automatically after every redis command
func (c *Peer) Flush() {
	c.w.Flush()
}

// Close the client connection after the current command is done.
func (c *Peer) Close() {
	c.closed = true
}

// WriteError writes a redis 'Error'
func (c *Peer) WriteError(e string) {
	fmt.Fprintf(c.w, "-%s\r\n", toInline(e))
}

// WriteInline writes a redis inline string
func (c *Peer) WriteInline(s string) {
	fmt.Fprintf(c.w, "+%s\r\n", toInline(s))
}

// WriteOK write the inline string `OK`
func (c *Peer) WriteOK() {
	c.WriteInline("OK")
}

// WriteBulk writes a bulk string
func (c *Peer) WriteBulk(s string) {
	fmt.Fprintf(c.w, "$%d\r\n%s\r\n", len(s), s)
}

// WriteNull writes a redis Null element
func (c *Peer) WriteNull() {
	fmt.Fprintf(c.w, "$-1\r\n")
}

// WriteLen starts an array with the given length
func (c *Peer) WriteLen(n int) {
	fmt.Fprintf(c.w, "*%d\r\n", n)
}

// WriteInt writes an integer
func (c *Peer) WriteInt(i int) {
	fmt.Fprintf(c.w, ":%d\r\n", i)
}

func toInline(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
}
