package cachetest

import (
	"context"
	"flag"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/champ-isaac/cacheconnector"
	"github.com/champ-isaac/cacheconnector/inmemory"
)

func openInmemory(b *testing.B) cache.Cache {
	c := inmemory.NewCache(inmemory.New())
	b.Cleanup(func() { _ = c.Close() })
	return c
}

type truncating struct{ cache.Cache }

func (t truncating) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := t.Cache.Get(ctx, key)
	if len(v) > 0 {
		v = v[:len(v)-1]
	}
	return v, err
}

func oneIteration(t *testing.T) {
	t.Helper()
	f := flag.Lookup("test.benchtime")
	require.NotNil(t, f)
	old := f.Value.String()
	require.NoError(t, f.Value.Set("1x"))
	t.Cleanup(func() { _ = f.Value.Set(old) })
}

func TestBenchCasesPass(t *testing.T) {
	oneIteration(t)
	cases := benchCases(openInmemory)
	require.Len(t, cases, 10)
	for _, c := range cases {
		assert.NotZero(t, testing.Benchmark(c.run).N, "%s failed", c.name)
	}
}

func TestBenchCasesCatchTruncatedValues(t *testing.T) {
	oneIteration(t)
	open := func(b *testing.B) cache.Cache {
		c := truncating{inmemory.NewCache(inmemory.New())}
		b.Cleanup(func() { _ = c.Close() })
		return c
	}
	checked := 0
	for _, c := range benchCases(open) {
		if !strings.HasPrefix(c.name, "GetHit") {
			continue
		}
		checked++
		assert.Zero(t, testing.Benchmark(c.run).N, "%s passed on a cache that truncates values", c.name)
	}
	assert.Equal(t, 4, checked, "three sequential sizes and one parallel case")
}

func TestSetBenchCasesCatchTruncatedValues(t *testing.T) {
	oneIteration(t)
	open := func(b *testing.B) cache.Cache {
		c := truncating{inmemory.NewCache(inmemory.New())}
		b.Cleanup(func() { _ = c.Close() })
		return c
	}
	for _, c := range benchCases(open) {
		if strings.HasPrefix(c.name, "Set/") {
			assert.Zero(t, testing.Benchmark(c.run).N, "%s passed on a cache that truncates values", c.name)
		}
	}
}
