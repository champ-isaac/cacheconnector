//go:build integration

package memcached

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/champ-isaac/cacheconnector/memcached"
)

const (
	MemcachedHost = "127.0.0.1:11211"
)

var (
	ctx    = context.Background()
	client *memcached.Client
	raw    *memcache.Client // used only to flush the server around the run
)

// newCache opens a new connection to the memcached backend.
func newCache(t *testing.T) *memcached.Client {
	t.Helper()
	return memcached.New([]string{MemcachedHost}, memcached.WithTimeout(2*time.Second))
}

// itemCount returns the number of live items on the server. It counts the
// keys in a metadump, not the curr_items stat: curr_items keeps counting
// flushed items until the server reclaims them, which would make a run that
// follows another one look like it found foreign data.
func itemCount() (int, error) {
	lines, err := metadump()
	return len(lines), err
}

// metadump returns one "key=..." line for each live item on the server. A line
// has fields such as exp= (the expiry as an absolute Unix time, -1 for none)
// and la= (the last access time).
func metadump() ([]string, error) {
	conn, err := net.DialTimeout("tcp", MemcachedHost, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := fmt.Fprint(conn, "lru_crawler metadump all\r\n"); err != nil {
		return nil, err
	}
	var lines []string
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		switch line := sc.Text(); {
		case line == "END":
			return lines, nil
		case strings.HasPrefix(line, "key="):
			lines = append(lines, line)
		}
	}
	return nil, errors.New("memcached metadump did not end with END")
}

// serverStat returns a numeric value from the server's stats, such as
// curr_connections.
func serverStat(name string) (int, error) {
	conn, err := net.DialTimeout("tcp", MemcachedHost, 2*time.Second)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := fmt.Fprint(conn, "stats\r\n"); err != nil {
		return 0, err
	}
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := sc.Text()
		if line == "END" {
			break
		}
		if v, ok := strings.CutPrefix(line, "STAT "+name+" "); ok {
			return strconv.Atoi(strings.TrimSpace(v))
		}
	}
	return 0, fmt.Errorf("memcached stat %q not found", name)
}

func TestMain(m *testing.M) {
	client = memcached.New(
		[]string{MemcachedHost},
		memcached.WithTimeout(2*time.Second),
		memcached.WithMaxIdleConns(8),
	)
	if err := client.Ping(ctx); err != nil {
		panic(err)
	}
	// Memcached has no databases, so the flush below wipes the whole server.
	// Only do it on an empty server, or when MEMCACHED_TEST_FLUSH=1 says the
	// data on it is disposable.
	n, err := itemCount()
	if err != nil {
		panic(err)
	}
	if n > 0 && os.Getenv("MEMCACHED_TEST_FLUSH") != "1" {
		fmt.Fprintf(os.Stderr, "memcached on %s holds %d items; not flushing them. "+
			"Set MEMCACHED_TEST_FLUSH=1 to let the tests wipe the server.\n", MemcachedHost, n)
		os.Exit(1)
	}
	raw = memcache.New(MemcachedHost)
	if err := raw.FlushAll(); err != nil {
		panic(err)
	}
	exitCode := m.Run()

	//teardown
	_ = raw.FlushAll()
	_ = client.Close()
	os.Exit(exitCode)
}
