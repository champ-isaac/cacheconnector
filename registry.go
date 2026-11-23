package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	// EnvBackend names the backend FromEnv opens. The default is "inmemory".
	EnvBackend = "CACHE_BACKEND"
	// EnvURL when set, is the DSN FromEnv opens and EnvBackend is ignored.
	EnvURL = "CACHE_URL"

	defaultBackend = "inmemory"
)

// Factory builds a Cache from a parsed DSN. When FromEnv selects a backend
// by name the URL has only a scheme, and the factory reads its own settings
// from the environment.
type Factory func(ctx context.Context, u *url.URL) (Cache, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes a backend available under a DSN scheme. A backend package
// calls it from init. It panics on an empty scheme, a nil factory or a
// scheme registered twice, which are programming errors.
func Register(scheme string, f Factory) {
	scheme = strings.ToLower(scheme)
	if scheme == "" || f == nil {
		panic("cache: Register needs a scheme and a factory")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[scheme]; dup {
		panic("cache: Register called twice for scheme " + scheme)
	}
	factories[scheme] = f
}

// Backends lists the registered schemes in sorted order.
func Backends() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Open builds a Cache from a DSN such as "redis://:pw@host:6379/1",
// "valkey://host:6379", "memcached://host:11211" or "inmemory:". The scheme
// picks the backend; see each backend package for the rest of its DSN. The
// error never contains the DSN, because it may carry a password.
func Open(ctx context.Context, dsn string) (Cache, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("cache: invalid DSN: %w", redactURLError(err))
	}
	return open(ctx, u)
}

// Config selects a backend by name. It is what a config file holds, and a
// project can embed it in its own config struct (it carries yaml tags):
//
//	driver: redis                    # inmemory, redis, valkey or memcached; default inmemory
//	url: redis://:pw@host:6379/1     # optional DSN; its scheme must agree with driver
//
// With only a driver name, the backend reads its own settings from its
// environment variables, for example REDIS_ADDR.
type Config struct {
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
}

// FromConfig opens the backend that c names. Backends must be registered
// first, usually by importing the "all" package. When both URL and Driver are
// set, the URL scheme must be the driver name or its TLS variant (the name
// plus "s", as in redis and rediss); otherwise it is an error.
func FromConfig(ctx context.Context, c Config) (Cache, error) {
	if dsn, name := strings.TrimSpace(c.URL), strings.ToLower(strings.TrimSpace(c.Driver)); dsn != "" && name != "" {
		// A parse error is left for Open to report, without the DSN.
		if u, err := url.Parse(dsn); err == nil {
			if scheme := strings.ToLower(u.Scheme); scheme != name && scheme != name+"s" {
				return nil, fmt.Errorf("cache: driver %s does not match the url scheme %q", shown(name), scheme)
			}
		}
	}
	return openConfig(ctx, c)
}

// openConfig is FromConfig without the agreement check: a URL, when set,
// wins. FromEnv keeps this documented rule for CACHE_URL over CACHE_BACKEND.
func openConfig(ctx context.Context, c Config) (Cache, error) {
	if dsn := strings.TrimSpace(c.URL); dsn != "" {
		return Open(ctx, dsn)
	}
	name := strings.ToLower(strings.TrimSpace(c.Driver))
	if name == "" {
		name = defaultBackend
	}
	return open(ctx, &url.URL{Scheme: name})
}

// FromFile reads a YAML file holding a Config and opens the backend it names.
// Unknown keys are an error, so a typo does not silently select the default.
// A file that sets neither driver nor url is an error too, as is a file with
// more than one YAML document. The error never contains file contents,
// because they may hold a password.
func FromFile(ctx context.Context, path string) (Cache, error) {
	// path is operator-supplied configuration (not untrusted input) and may
	// legitimately be any path, so os.Root scoping does not apply.
	b, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- trusted config path
	if err != nil {
		return nil, fmt.Errorf("cache: read config: %w", err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	switch err := dec.Decode(&c); {
	case errors.Is(err, io.EOF): // empty file; caught below
	case err != nil:
		return nil, fmt.Errorf("cache: parse config %s: %w", path, err)
	default:
		var extra yaml.Node
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("cache: config %s has more than one YAML document", path)
		}
	}
	if strings.TrimSpace(c.Driver) == "" && strings.TrimSpace(c.URL) == "" {
		return nil, fmt.Errorf("cache: config %s sets neither driver nor url", path)
	}
	return FromConfig(ctx, c)
}

// FromEnv opens the cache named by CACHE_URL, or else by CACHE_BACKEND
// (default "inmemory"), in which case the backend reads its own settings
// from its environment variables, for example REDIS_ADDR.
func FromEnv(ctx context.Context) (Cache, error) {
	return openConfig(ctx, Config{Driver: os.Getenv(EnvBackend), URL: os.Getenv(EnvURL)})
}

func open(ctx context.Context, u *url.URL) (Cache, error) {
	scheme := strings.ToLower(u.Scheme)
	mu.RLock()
	f, ok := factories[scheme]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cache: unknown backend %s (registered: %s); did you import its package?",
			shown(scheme), strings.Join(Backends(), ", "))
	}
	return f(ctx, u)
}

// shown quotes a driver name for an error message. A value that is not a
// plain name, such as a DSN put in driver by mistake, may hold a password, so
// it is not echoed.
func shown(name string) string {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '-', r == '.', r == '_':
		default:
			return "(not a valid name)"
		}
	}
	return strconv.Quote(name)
}

// redactURLError drops the URL that url.Parse errors quote, since a DSN can
// hold a password.
func redactURLError(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}
