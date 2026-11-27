package cache

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var ctx = context.Background()

type stub struct{ u *url.URL }

func (stub) Get(context.Context, string) ([]byte, error)              { return nil, ErrNotFound }
func (stub) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (stub) Delete(context.Context, string) error                     { return nil }
func (stub) Ping(context.Context) error                               { return nil }
func (stub) Close() error                                             { return nil }

func reset(t *testing.T) {
	t.Helper()
	mu.Lock()
	saved := factories
	factories = map[string]Factory{}
	mu.Unlock()
	savedTLS := tlsVariant
	tlsVariant = map[string]string{}
	t.Cleanup(func() { mu.Lock(); factories = saved; tlsVariant = savedTLS; mu.Unlock() })
}

func stubFactory(_ context.Context, u *url.URL) (Cache, error) { return stub{u}, nil }

func TestRegister(t *testing.T) {
	reset(t)
	Register("Fake", stubFactory)
	assert.Equal(t, []string{"fake"}, Backends())
	assert.Panics(t, func() { Register("fake", stubFactory) }, "duplicate")
	assert.Panics(t, func() { Register("", stubFactory) }, "empty scheme")
	assert.Panics(t, func() { Register("other", nil) }, "nil factory")
}

func TestOpen(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)
	Register("alpha", stubFactory)

	c, err := Open(ctx, "FAKE://host:1234/3?x=y")
	require.NoError(t, err)
	u := c.(stub).u
	assert.Equal(t, "host:1234", u.Host)
	assert.Equal(t, "/3", u.Path)

	_, err = Open(ctx, "nope://h")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown backend "nope"`)
	assert.Contains(t, err.Error(), "alpha, fake")
}

func TestOpen_BadDSNDoesNotLeakPassword(t *testing.T) {
	_, err := Open(ctx, "redis://:s3cret@host:notaport")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")
	assert.NotContains(t, redactURLError(errors.New("plain")).Error(), "s3cret")
}

func TestOpen_FactoryError(t *testing.T) {
	reset(t)
	boom := errors.New("boom")
	Register("bad", func(context.Context, *url.URL) (Cache, error) { return nil, boom })
	_, err := Open(ctx, "bad:")
	assert.ErrorIs(t, err, boom)
}

func TestFromEnv(t *testing.T) {
	reset(t)
	Register("inmemory", stubFactory)
	Register("fake", stubFactory)

	t.Run("default is inmemory", func(t *testing.T) {
		t.Setenv(EnvBackend, "")
		t.Setenv(EnvURL, "")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		assert.Equal(t, "inmemory", c.(stub).u.Scheme)
	})
	t.Run("backend name", func(t *testing.T) {
		t.Setenv(EnvBackend, " Fake ")
		t.Setenv(EnvURL, "")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		u := c.(stub).u
		assert.Equal(t, "fake", u.Scheme)
		assert.Empty(t, u.Host, "named backends read their own environment")
	})
	t.Run("url wins over backend", func(t *testing.T) {
		t.Setenv(EnvBackend, "inmemory")
		t.Setenv(EnvURL, "fake://h:1")
		c, err := FromEnv(ctx)
		require.NoError(t, err)
		assert.Equal(t, "h:1", c.(stub).u.Host)
	})
	t.Run("unknown backend", func(t *testing.T) {
		t.Setenv(EnvBackend, "nope")
		t.Setenv(EnvURL, "")
		_, err := FromEnv(ctx)
		assert.Error(t, err)
	})
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cache.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestFromFile(t *testing.T) {
	reset(t)
	Register("inmemory", stubFactory)
	Register("fake", stubFactory)

	t.Run("driver name", func(t *testing.T) {
		c, err := FromFile(ctx, writeConfig(t, "driver: Fake\n"))
		require.NoError(t, err)
		assert.Equal(t, "fake", c.(stub).u.Scheme)
	})
	t.Run("url with a matching driver", func(t *testing.T) {
		c, err := FromFile(ctx, writeConfig(t, "driver: fake\nurl: fake://h:1\n"))
		require.NoError(t, err)
		assert.Equal(t, "h:1", c.(stub).u.Host)
	})
	t.Run("unknown driver", func(t *testing.T) {
		_, err := FromFile(ctx, writeConfig(t, "driver: nope\n"))
		assert.ErrorContains(t, err, `unknown backend "nope"`)
	})
	t.Run("typo in key", func(t *testing.T) {
		_, err := FromFile(ctx, writeConfig(t, "drivr: fake\n"))
		assert.ErrorContains(t, err, "invalid YAML content")
	})
	t.Run("bad yaml does not leak secrets", func(t *testing.T) {
		_, err := FromFile(ctx, writeConfig(t, "url: [redis://:s3cret@h\n"))
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "s3cret")
	})
	t.Run("missing file", func(t *testing.T) {
		_, err := FromFile(ctx, filepath.Join(t.TempDir(), "none.yaml"))
		assert.ErrorContains(t, err, "read config")
	})
}

func TestFromFile_DriverHoldingDSNDoesNotLeak(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)
	_, err := FromFile(ctx, writeConfig(t, "driver: redis://:s3cret@h:1\n"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")
	assert.NotContains(t, err.Error(), "redis://")
}

func TestFromFile_URLAndDriverDisagree(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)
	Register("other", stubFactory)

	_, err := FromFile(ctx, writeConfig(t, "driver: other\nurl: fake://:s3cret@h:1\n"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")
}

func TestFromFile_NeitherDriverNorURL(t *testing.T) {
	reset(t)
	Register("inmemory", stubFactory)
	for name, body := range map[string]string{
		"empty file":    "",
		"comments only": "# nothing here\n",
		"blank values":  "driver: \"  \"\nurl: \"\"\n",
	} {
		_, err := FromFile(ctx, writeConfig(t, body))
		assert.Error(t, err, name)
	}
}

func TestFromFile_SecondDocumentRejected(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)
	Register("other", stubFactory)
	_, err := FromFile(ctx, writeConfig(t, "driver: fake\n---\ndriver: other\n"))
	assert.ErrorContains(t, err, "more than one YAML document")
}

func TestResolved(t *testing.T) {
	reset(t)
	Register("inmemory", stubFactory)
	Register("fake", stubFactory)
	Register("fakes", stubFactory)
	Register("redis", stubFactory, WithTLS("rediss"))
	Register("rediss", stubFactory)

	opened := func(c Cache) string { return c.(stub).u.Scheme }

	t.Run("from a file", func(t *testing.T) {
		for body, want := range map[string]string{
			"driver: Fake\n":                       "fake",
			"url: \"fakes://h:1\"\n":               "fakes",
			"driver: redis\nurl: \"rediss://h\"\n": "rediss",
		} {
			path := writeConfig(t, body)
			cfg, err := ReadConfig(path)
			require.NoError(t, err)
			assert.Equal(t, want, cfg.Resolved(), body)
			c, err := FromFile(ctx, path)
			require.NoError(t, err)
			assert.Equal(t, opened(c), cfg.Resolved(), "Resolved matches what FromFile opened")
		}
		_, err := ReadConfig(writeConfig(t, ""))
		assert.ErrorContains(t, err, "neither driver nor url")
	})
	t.Run("from a config", func(t *testing.T) {
		for cfg, want := range map[Config]string{
			{Driver: " Fake "}:                   "fake",
			{URL: "fakes://h:1"}:                 "fakes",
			{Driver: "redis", URL: "rediss://h"}: "rediss",
			{}:                                   "inmemory",
		} {
			assert.Equal(t, want, cfg.Resolved(), "%+v", cfg)
			c, err := FromConfig(ctx, cfg)
			require.NoError(t, err)
			assert.Equal(t, opened(c), cfg.Resolved(), "Resolved matches what FromConfig opened")
		}
	})
	t.Run("never returns a DSN", func(t *testing.T) {
		assert.Empty(t, Config{Driver: "redis://:s3cret@h:1"}.Resolved())
		assert.Empty(t, Config{URL: "redis://:s3cret@h:notaport"}.Resolved())
	})
}

func TestFromFile_TLSVariantOnlyForDeclaredPairs(t *testing.T) {
	reset(t)
	Register("redis", stubFactory, WithTLS("rediss"))
	Register("valkey", stubFactory, WithTLS("valkeys"))
	for _, name := range []string{"rediss", "valkeys", "fake", "fakes"} {
		Register(name, stubFactory)
	}
	for body, ok := range map[string]bool{
		"driver: redis\nurl: \"rediss://h\"\n":   true,
		"driver: valkey\nurl: \"valkeys://h\"\n": true,
		"driver: redi\nurl: \"redis://h\"\n":     false,
		"driver: fake\nurl: \"fakes://h\"\n":     false,
		"driver: rediss\nurl: \"redis://h\"\n":   false,
	} {
		_, err := FromFile(ctx, writeConfig(t, body))
		if ok {
			assert.NoError(t, err, body)
		} else {
			assert.ErrorContains(t, err, "does not match the url scheme", body)
		}
	}
}

func TestFromFile_YAMLErrorsDoNotEchoFileText(t *testing.T) {
	reset(t)
	Register("inmemory", stubFactory)
	for body, stage := range map[string]string{
		"url: !!int s3cret\n":       "YAML syntax error",
		"url: *s3cret\n":            "YAML syntax error",
		"s3cret: x\n":               "invalid YAML content",
		"url: [redis://:s3cret@h\n": "YAML syntax error",
	} {
		path := writeConfig(t, body)
		_, err := FromFile(ctx, path)
		require.Error(t, err, body)
		assert.NotContains(t, err.Error(), "s3cret", body)
		assert.Contains(t, err.Error(), path, "names the file")
		assert.Contains(t, err.Error(), stage, body)
	}
}

func TestFromFile_TrailingAndBrokenDocuments(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)

	for _, body := range []string{"driver: fake\n---\n", "driver: fake\n---\n# note\n", "driver: fake\n---\n---\n"} {
		_, err := FromFile(ctx, writeConfig(t, body))
		assert.NoError(t, err, "%q", body)
	}
	_, err := FromFile(ctx, writeConfig(t, "driver: fake\n---\n[x\n"))
	assert.ErrorContains(t, err, "YAML syntax error")
	assert.NotContains(t, err.Error(), "more than one")
	_, err = FromFile(ctx, writeConfig(t, "driver: fake\n---\n~\n"))
	assert.ErrorContains(t, err, "more than one YAML document", "an explicit null is content")
}

func TestSchemeLessDSNDoesNotLeak(t *testing.T) {
	reset(t)
	Register("redis", stubFactory)
	const dsn = "admin:hunter2@host:6379"
	leaks := func(t *testing.T, s string) {
		t.Helper()
		assert.NotContains(t, s, "admin")
		assert.NotContains(t, s, "hunter2")
	}

	t.Run("unknown backend, from a file", func(t *testing.T) {
		_, err := FromFile(ctx, writeConfig(t, "url: \""+dsn+"\"\n"))
		require.ErrorContains(t, err, "unknown backend")
		leaks(t, err.Error())
	})
	t.Run("unknown backend, from Open", func(t *testing.T) {
		_, err := Open(ctx, dsn)
		require.ErrorContains(t, err, "unknown backend")
		leaks(t, err.Error())
	})
	t.Run("driver and url disagree", func(t *testing.T) {
		_, err := FromConfig(ctx, Config{Driver: "redis", URL: dsn})
		require.ErrorContains(t, err, "does not match the url scheme")
		leaks(t, err.Error())
	})
	t.Run("Resolved", func(t *testing.T) {
		assert.Empty(t, Config{URL: dsn}.Resolved())
	})
	t.Run("a real scheme is still named", func(t *testing.T) {
		_, err := Open(ctx, "nope://h")
		assert.ErrorContains(t, err, `unknown backend "nope"`)
	})
}

func TestEmptyDocument(t *testing.T) {
	parse := func(body string) *yaml.Node {
		var n yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(body), &n))
		return &n
	}
	null := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}

	assert.True(t, emptyDocument(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{null}}), "empty document")
	assert.False(t, emptyDocument(parse("a: 1\n")), "mapping")
	assert.False(t, emptyDocument(parse("~\n")), "explicit null is content")
	assert.False(t, emptyDocument(null), "not a document node")
	assert.False(t, emptyDocument(&yaml.Node{Kind: yaml.DocumentNode}), "document with no child")
	assert.False(t, emptyDocument(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{null, null}}), "document with two children")
}

func TestFromFile_ContentErrorDescribesEveryCause(t *testing.T) {
	reset(t)
	Register("inmemory", stubFactory)
	for name, body := range map[string]string{
		"duplicate key":   "driver: inmemory\ndriver: redis\n",
		"scalar document": "inmemory\n",
		"list document":   "- driver: inmemory\n",
		"unknown key":     "drivr: inmemory\n",
		"wrong value":     "driver: [inmemory]\n",
	} {
		_, err := FromFile(ctx, writeConfig(t, body))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "invalid YAML content", name)
		assert.Contains(t, err.Error(), "one mapping", name)
		assert.Contains(t, err.Error(), "each set once", name)
		assert.NotContains(t, err.Error(), "unknown key or wrong value type", name)
	}
	_, err := FromFile(ctx, writeConfig(t, "url: [x\n"))
	assert.ErrorContains(t, err, "YAML syntax error", "other errors are unchanged")
}

func TestFromFile_LeadingEmptyDocumentsSkipped(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)

	for _, body := range []string{
		"---\n---\ndriver: fake\n",
		"---\n# note\n---\n---\ndriver: fake\n",
		"---\n---\ndriver: fake\n---\n",
	} {
		c, err := FromFile(ctx, writeConfig(t, body))
		require.NoError(t, err, "%q", body)
		assert.Equal(t, "fake", c.(stub).u.Scheme, "%q", body)
	}
	_, err := FromFile(ctx, writeConfig(t, "---\n---\ndriver: fake\n---\ndriver: fake\n"))
	assert.ErrorContains(t, err, "more than one YAML document", "two documents with content")
	_, err = FromFile(ctx, writeConfig(t, "---\n---\n"))
	assert.ErrorContains(t, err, "neither driver nor url", "only empty documents")
	_, err = FromFile(ctx, writeConfig(t, "---\n---\ndrivr: fake\n"))
	assert.ErrorContains(t, err, "invalid YAML content", "the config document is still checked")
}

func TestConfigEntryPointDefaults(t *testing.T) {
	reset(t)
	Register("inmemory", stubFactory)

	_, err := FromFile(ctx, writeConfig(t, "# nothing\n"))
	assert.ErrorContains(t, err, "neither driver nor url")
	c, err := FromConfig(ctx, Config{})
	require.NoError(t, err)
	assert.Equal(t, "inmemory", c.(stub).u.Scheme)
	t.Setenv(EnvBackend, "")
	t.Setenv(EnvURL, "")
	c, err = FromEnv(ctx)
	require.NoError(t, err)
	assert.Equal(t, "inmemory", c.(stub).u.Scheme)
}

func TestRegisterWithTLS(t *testing.T) {
	reset(t)
	mu.RLock()
	assert.Empty(t, tlsVariant, "no built-in pairs")
	mu.RUnlock()
	Register("mystore", stubFactory, WithTLS("MyStoreS"))
	Register("mystores", stubFactory)
	Register("plain", stubFactory)
	Register("plains", stubFactory)
	Register("redis", stubFactory)
	Register("rediss", stubFactory)

	for body, ok := range map[string]bool{
		"driver: mystore\nurl: \"mystores://h\"\n": true,
		"driver: mystore\nurl: \"mystore://h\"\n":  true,
		"driver: plain\nurl: \"plains://h\"\n":     false,
		"driver: mystore\nurl: \"plains://h\"\n":   false,
		"driver: redis\nurl: \"rediss://h\"\n":     false,
	} {
		_, err := FromFile(ctx, writeConfig(t, body))
		if ok {
			assert.NoError(t, err, body)
		} else {
			assert.ErrorContains(t, err, "does not match the url scheme", body)
		}
	}

	assert.Panics(t, func() { Register("a", stubFactory, WithTLS("")) }, "empty TLS scheme")
	assert.Panics(t, func() { Register("b", stubFactory, WithTLS("B")) }, "TLS scheme equal to the scheme")
	assert.Panics(t, func() { Register("mystore", stubFactory, WithTLS("x")) }, "duplicate")
	mu.RLock()
	assert.Equal(t, "mystores", tlsVariant["mystore"], "a failed Register leaves the pairing as it was")
	assert.NotContains(t, tlsVariant, "a")
	assert.NotContains(t, tlsVariant, "b")
	mu.RUnlock()
}
