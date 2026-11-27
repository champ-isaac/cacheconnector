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
	EnvBackend = "CACHE_BACKEND"

	EnvURL = "CACHE_URL"

	defaultBackend = "inmemory"
)

type Factory func(ctx context.Context, u *url.URL) (Cache, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

type Option func(*registration)

type registration struct {
	tls    string
	hasTLS bool
}

func WithTLS(scheme string) Option {
	return func(r *registration) { r.tls, r.hasTLS = strings.ToLower(scheme), true }
}

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

func Open(ctx context.Context, dsn string) (Cache, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("cache: invalid DSN: %w", redactURLError(err))
	}
	return open(ctx, u)
}

type Config struct {
	Driver string `yaml:"driver"`
	URL    string `yaml:"url"`
}

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

func FromConfig(ctx context.Context, c Config) (Cache, error) {
	if dsn, name := strings.TrimSpace(c.URL), strings.ToLower(strings.TrimSpace(c.Driver)); dsn != "" && name != "" {
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

var tlsVariant = map[string]string{}

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

func FromFile(ctx context.Context, path string) (Cache, error) {
	c, err := ReadConfig(path)
	if err != nil {
		return nil, err
	}
	return FromConfig(ctx, c)
}

func ReadConfig(path string) (Config, error) {
	b, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- trusted config path
	if err != nil {
		return Config{}, fmt.Errorf("cache: read config: %w", err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	for i := leadingEmpty(b); i > 0; i-- {
		var skip yaml.Node
		_ = dec.Decode(&skip)
	}
	switch err := dec.Decode(&c); {
	case errors.Is(err, io.EOF):
	case err != nil:
		return Config{}, yamlError(path, err)
	default:

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

func yamlError(path string, err error) error {
	var te *yaml.TypeError
	if errors.As(err, &te) {
		return fmt.Errorf("cache: parse config %s: invalid YAML content (expected one mapping with only the keys \"driver\" and \"url\", each set once, and text values)", path)
	}
	return fmt.Errorf("cache: parse config %s: YAML syntax error", path)
}

func emptyDocument(n *yaml.Node) bool {
	if n.Kind != yaml.DocumentNode || len(n.Content) != 1 {
		return false
	}
	v := n.Content[0]
	return v.Kind == yaml.ScalarNode && v.Tag == "!!null" && v.Value == ""
}

func shown(name string) string {
	if !plainName(name) {
		return notAName
	}
	return strconv.Quote(name)
}

const notAName = "(not a valid name)"

func shownScheme(u *url.URL) string {
	if u.Opaque != "" {
		return notAName
	}
	return shown(strings.ToLower(u.Scheme))
}

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

func redactURLError(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}
