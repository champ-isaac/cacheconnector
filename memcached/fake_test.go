package memcached

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeServer struct {
	ln net.Listener

	mu    sync.Mutex
	data  map[string]fakeItem
	clock time.Time

	lastExptime int64
}

type fakeItem struct {
	value   []byte
	expires time.Time
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{ln: ln, data: map[string]fakeItem{}, clock: time.Unix(1_700_000_000, 0)}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeServer) Addr() string { return s.ln.Addr().String() }

func (s *fakeServer) now() time.Time { s.mu.Lock(); defer s.mu.Unlock(); return s.clock }

func (s *fakeServer) advance(d time.Duration) {
	s.mu.Lock()
	s.clock = s.clock.Add(d)
	s.mu.Unlock()
}

func (s *fakeServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "version":
			reply(conn, "VERSION 1.6.0-fake\r\n")
		case "gets":
			s.mu.Lock()
			for _, key := range f[1:] {
				it, ok := s.data[key]
				if !ok || (!it.expires.IsZero() && !s.clock.Before(it.expires)) {
					continue
				}
				reply(conn, "VALUE %s 0 %d 1\r\n%s\r\n", key, len(it.value), it.value)
			}
			s.mu.Unlock()
			reply(conn, "END\r\n")
		case "set":
			n, _ := strconv.Atoi(f[4])
			exp, _ := strconv.ParseInt(f[3], 10, 64)
			buf := make([]byte, n+2)
			if _, err := io.ReadFull(r, buf); err != nil {
				return
			}
			s.mu.Lock()
			it := fakeItem{value: buf[:n]}
			switch {
			case exp == 0:
			case exp > int64(maxRelativeTTL/time.Second):
				it.expires = time.Unix(exp, 0)
			default:
				it.expires = s.clock.Add(time.Duration(exp) * time.Second)
			}
			s.data[f[1]] = it
			s.lastExptime = exp
			s.mu.Unlock()
			reply(conn, "STORED\r\n")
		case "delete":
			s.mu.Lock()
			_, ok := s.data[f[1]]
			delete(s.data, f[1])
			s.mu.Unlock()
			if ok {
				reply(conn, "DELETED\r\n")
			} else {
				reply(conn, "NOT_FOUND\r\n")
			}
		default:
			reply(conn, "ERROR\r\n")
		}
	}
}

func reply(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
