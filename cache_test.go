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
)

var ctx = context.Background()

type stub struct{ u *url.URL }

func (stub) Get(context.Context, string) ([]byte, error)              { return nil, ErrNotFound }
func (stub) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (stub) Delete(context.Context, string) error                     { return nil }
func (stub) Ping(context.Context) error                               { return nil }
func (stub) Close() error                                             { return nil }

// reset runs a test against an empty registry and restores it afterwards.
func reset(t *testing.T) {
	t.Helper()
	mu.Lock()
	saved := factories
	factories = map[string]Factory{}
	mu.Unlock()
	t.Cleanup(func() { mu.Lock(); factories = saved; mu.Unlock() })
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
		assert.ErrorContains(t, err, "drivr")
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

// DES-1: a DSN put in driver by mistake must not reach the error text.
func TestFromFile_DriverHoldingDSNDoesNotLeak(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)
	_, err := FromFile(ctx, writeConfig(t, "driver: redis://:s3cret@h:1\n"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")
	assert.NotContains(t, err.Error(), "redis://")
}

// DES-2: url and driver that disagree are an error, and the error does not
// echo the url.
func TestFromFile_URLAndDriverDisagree(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)
	Register("fakes", stubFactory)
	Register("other", stubFactory)

	_, err := FromFile(ctx, writeConfig(t, "driver: other\nurl: fake://:s3cret@h:1\n"))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")

	_, err = FromFile(ctx, writeConfig(t, "driver: fake\nurl: fakes://h:1\n"))
	assert.NoError(t, err, "a TLS variant of the driver's scheme agrees")
}

// DES-3: a file that names neither driver nor url is an error, not a silent
// default.
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

// DES-5: a second YAML document must not be silently ignored.
func TestFromFile_SecondDocumentRejected(t *testing.T) {
	reset(t)
	Register("fake", stubFactory)
	Register("other", stubFactory)
	_, err := FromFile(ctx, writeConfig(t, "driver: fake\n---\ndriver: other\n"))
	assert.ErrorContains(t, err, "more than one YAML document")
}
