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

// Option changes how Register records a backend. Register takes none by
// default, so callers that pass no option are unaffected.
type Option func(*registration)

type registration struct {
	tls    string
	hasTLS bool
}

// WithTLS declares the scheme that adds TLS to the backend being registered,
// such as "mystores" for "mystore". A config that sets both driver and url
// then accepts the url scheme "mystores" next to driver "mystore". The TLS
// scheme still needs its own Register call; WithTLS only records the pair.
func WithTLS(scheme string) Option {
	return func(r *registration) { r.tls, r.hasTLS = strings.ToLower(scheme), true }
}

// Register makes a backend available under a DSN scheme. A backend package
// calls it from init, optionally with WithTLS. It panics on an empty scheme,
// a nil factory, a scheme registered twice, or a WithTLS scheme that is empty
// or the same as the scheme, which are programming errors. A panic leaves the
// registry unchanged.
func Register(scheme string, f Factory, opts ...Option) {
	scheme = strings.ToLower(scheme)
	if scheme == "" || f == nil {
		panic("cache: Register needs a scheme and a factory")
	}
	var r registration
	for _, o := range opts {
		o(&r)
	}
	if r.hasTLS && (r.tls == "" || r.tls == scheme) {
		panic("cache: WithTLS needs a scheme different from " + scheme)
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[scheme]; dup {
		panic("cache: Register called twice for scheme " + scheme)
	}
	factories[scheme] = f
	if r.hasTLS {
		tlsVariant[scheme] = r.tls
	}
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
//	driver: redis                    # inmemory, redis, valkey or memcached
//	url: redis://:pw@host:6379/1     # optional DSN; its scheme must agree with driver
//
// The entry points treat it differently. FromFile (through ReadConfig)
// requires at least one of driver and url. FromConfig accepts an empty Config
// and opens "inmemory", and when both are set it requires the url scheme to
// agree with driver. FromEnv builds a Config from CACHE_BACKEND and
// CACHE_URL, opens "inmemory" when both are empty, and lets the url win
// without the agreement check.
//
// With only a driver name, the backend reads its own settings from its
// environment variables, for example REDIS_ADDR.
type Config struct {
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
}

// Resolved returns the backend name FromConfig would open for c: the url
// scheme when url is set, else driver, else "inmemory", in lower case. It is
// how a caller sees which driver is running, since Cache has no name method
// and %T cannot tell redis from valkey. It does not check that the backend
// is registered or that url and driver agree. It returns "" when the value is
// not a plain name or url does not parse, so a DSN never reaches a log line.
func (c Config) Resolved() string {
	name := strings.ToLower(strings.TrimSpace(c.Driver))
	if dsn := strings.TrimSpace(c.URL); dsn != "" {
		u, err := url.Parse(dsn)
		if err != nil || u.Opaque != "" {
			return ""
		}
		name = strings.ToLower(u.Scheme)
	} else if name == "" {
		name = defaultBackend
	}
	if !plainName(name) {
		return ""
	}
	return name
}

// FromConfig opens the backend that c names. Backends must be registered
// first, usually by importing the "all" package. When both URL and Driver are
// set, the URL scheme must be the driver name or its TLS variant (redis and
// rediss, valkey and valkeys); otherwise it is an error.
func FromConfig(ctx context.Context, c Config) (Cache, error) {
	if dsn, name := strings.TrimSpace(c.URL), strings.ToLower(strings.TrimSpace(c.Driver)); dsn != "" && name != "" {
		// A parse error is left for Open to report, without the DSN.
		if u, err := url.Parse(dsn); err == nil {
			mu.RLock()
			tls := tlsVariant[name]
			mu.RUnlock()
			if scheme := strings.ToLower(u.Scheme); scheme != name && scheme != tls {
				return nil, fmt.Errorf("cache: driver %s does not match the url scheme %s", shown(name), shownScheme(u))
			}
		}
	}
	return openConfig(ctx, c)
}

// tlsVariant maps a driver to the URL scheme that adds TLS to it, guarded by
// mu. It starts empty: the root package names no backend, and each backend
// adds its own pair through Register(..., WithTLS(...)).
var tlsVariant = map[string]string{}

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
// It is ReadConfig followed by FromConfig, so a caller that wants to see the
// driver first can call those two itself and use Config.Resolved.
func FromFile(ctx context.Context, path string) (Cache, error) {
	c, err := ReadConfig(path)
	if err != nil {
		return nil, err
	}
	return FromConfig(ctx, c)
}

// ReadConfig reads a YAML file holding a Config without opening anything.
// Unknown keys are an error, so a typo does not silently select the default.
// A file that sets neither driver nor url is an error too, as is a file with
// more than one YAML document that has content. Empty documents, such as a
// stray "---" before or after the config, are skipped. The error never
// contains file contents, because they may hold a password.
func ReadConfig(path string) (Config, error) {
	// path is operator-supplied configuration (not untrusted input) and may
	// legitimately be any path, so os.Root scoping does not apply.
	b, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- trusted config path
	if err != nil {
		return Config{}, fmt.Errorf("cache: read config: %w", err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	for i := leadingEmpty(b); i > 0; i-- {
		var skip yaml.Node
		_ = dec.Decode(&skip) // already parsed by leadingEmpty, so it cannot fail
	}
	switch err := dec.Decode(&c); {
	case errors.Is(err, io.EOF): // empty file; caught below
	case err != nil:
		return Config{}, yamlError(path, err)
	default:
		// Anything after the first document must be empty, such as a
		// trailing "---".
		for {
			var extra yaml.Node
			err := dec.Decode(&extra)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return Config{}, yamlError(path, err)
			}
			if !emptyDocument(&extra) {
				return Config{}, fmt.Errorf("cache: config %s has more than one YAML document", path)
			}
		}
	}
	if strings.TrimSpace(c.Driver) == "" && strings.TrimSpace(c.URL) == "" {
		return Config{}, fmt.Errorf("cache: config %s sets neither driver nor url", path)
	}
	return c, nil
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
			shownScheme(u), strings.Join(Backends(), ", "))
	}
	return f(ctx, u)
}

// leadingEmpty counts the empty documents before the first one with content.
// It reads the bytes with its own decoder, because a yaml.Node does not
// honor the strict unknown-key setting that ReadConfig needs for the config
// document itself. A parse error stops the count; ReadConfig then reports it
// when its own decoder reaches the same place.
func leadingEmpty(b []byte) int {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	n := 0
	for {
		var d yaml.Node
		if dec.Decode(&d) != nil || !emptyDocument(&d) {
			return n
		}
		n++
	}
}

// yamlError names the file and the stage that failed. It drops the parser's
// own message, because that can quote text from the file, such as a password.
func yamlError(path string, err error) error {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		// Every TypeError cause: an unknown or duplicate key, a document
		// that is not a mapping, or a value that is not text.
		return fmt.Errorf("cache: parse config %s: invalid YAML content (expected one mapping with only the keys \"driver\" and \"url\", each set once, and text values)", path)
	}
	return fmt.Errorf("cache: parse config %s: YAML syntax error", path)
}

// emptyDocument reports whether n is a document with no content, such as the
// one after a trailing "---". An explicit "~" or "" is content.
func emptyDocument(n *yaml.Node) bool {
	if n.Kind != yaml.DocumentNode || len(n.Content) != 1 {
		return false
	}
	v := n.Content[0]
	return v.Kind == yaml.ScalarNode && v.Tag == "!!null" && v.Value == ""
}

// shown quotes a driver name for an error message. A value that is not a
// plain name, such as a DSN put in driver by mistake, may hold a password, so
// it is not echoed.
func shown(name string) string {
	if !plainName(name) {
		return notAName
	}
	return strconv.Quote(name)
}

const notAName = "(not a valid name)"

// shownScheme is shown for the scheme of a parsed URL. A DSN written without
// its scheme, such as "admin:hunter2@host:6379", parses as scheme "admin" with
// an opaque rest, so a URL with an opaque part has no scheme worth printing:
// the "scheme" is really the start of a credential. Documented DSNs, such as
// "inmemory:" and "redis://host", have no opaque part.
func shownScheme(u *url.URL) string {
	if u.Opaque != "" {
		return notAName
	}
	return shown(strings.ToLower(u.Scheme))
}

// plainName reports whether name is only lower-case letters, digits and
// + - . _, so it is safe to print. The empty name counts as plain.
func plainName(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '-', r == '.', r == '_':
		default:
			return false
		}
	}
	return true
}

// redactURLError drops the URL that url.Parse errors quote, since a DSN can
// hold a password.
func redactURLError(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}
