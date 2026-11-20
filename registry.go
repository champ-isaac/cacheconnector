package cache

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
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

// FromEnv opens the cache named by CACHE_URL, or else by CACHE_BACKEND
// (default "inmemory"), in which case the backend reads its own settings
// from its environment variables, for example REDIS_ADDR.
func FromEnv(ctx context.Context) (Cache, error) {
	if dsn := strings.TrimSpace(os.Getenv(EnvURL)); dsn != "" {
		return Open(ctx, dsn)
	}
	name := strings.ToLower(strings.TrimSpace(os.Getenv(EnvBackend)))
	if name == "" {
		name = defaultBackend
	}
	return open(ctx, &url.URL{Scheme: name})
}

func open(ctx context.Context, u *url.URL) (Cache, error) {
	scheme := strings.ToLower(u.Scheme)
	mu.RLock()
	f, ok := factories[scheme]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cache: unknown backend %q (registered: %s); did you import its package?",
			scheme, strings.Join(Backends(), ", "))
	}
	return f(ctx, u)
}

// redactURLError drops the URL that url.Parse errors quote, since a DSN can
// hold a password.
func redactURLError(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}
