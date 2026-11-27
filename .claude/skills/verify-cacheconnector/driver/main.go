package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cache "github.com/champ-isaac/cacheconnector"
	_ "github.com/champ-isaac/cacheconnector/all"
)

type target struct {
	name string
	yaml string

	emptyKeyOK bool
}

var targets = []target{
	{"inmemory", "driver: inmemory\n", true},
	{"redis", "driver: redis\nurl: redis://default:user@127.0.0.1:6381/15\n", true},
	{"valkey", "driver: valkey\nurl: valkey://default:user@127.0.0.1:6380/15\n", true},
	{"memcached", "driver: memcached\nurl: memcached://127.0.0.1:11211\n", false},
}

var fails int

func check(ok bool, what, seen string) {
	tag := "PASS"
	if !ok {
		tag = "FAIL"
		fails++
	}
	fmt.Printf("  [%s] %s -> %s\n", tag, what, seen)
}

func main() {
	mode := flag.String("mode", "drive", "doctor or drive")
	out := flag.String("out", "", "directory for the per-backend config files")
	run := flag.String("run", "manual", "run id; every key gets the prefix verify-<run>:")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "need -out")
		os.Exit(2)
	}
	ctx := context.Background()
	if *mode == "doctor" {
		doctor(ctx, *out, *run)
	} else {
		registry(ctx, *out)
		for _, t := range targets {
			drive(ctx, t, *out, *run)
		}
	}
	if fails > 0 {
		fmt.Printf("\nRESULT: %d check(s) FAILED\n", fails)
		os.Exit(1)
	}
	fmt.Println("\nRESULT: all checks passed")
}

func open(ctx context.Context, t target, dir string) (cache.Cache, cache.Config, error) {
	path := filepath.Join(dir, t.name+".yaml")
	if err := os.WriteFile(path, []byte(t.yaml), 0o600); err != nil {
		return nil, cache.Config{}, err
	}
	cfg, err := cache.ReadConfig(path)
	if err != nil {
		return nil, cfg, err
	}
	c, err := cache.FromFile(ctx, path)
	return c, cfg, err
}

func doctor(ctx context.Context, dir, run string) {
	for _, t := range targets {
		fmt.Printf("doctor %s\n", t.name)
		c, cfg, err := open(ctx, t, dir)
		if err != nil {
			check(false, "open from config file", err.Error())
			continue
		}
		check(cfg.DriverName() == t.name, "Config.DriverName()", cfg.DriverName())
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = c.Ping(pctx)
		cancel()
		check(err == nil, "Ping", fmt.Sprint(err))
		k := "verify-" + run + ":doctor"
		err = c.Set(ctx, k, []byte("ok"), time.Minute)
		v, gerr := c.Get(ctx, k)
		check(err == nil && gerr == nil && string(v) == "ok", "Set+Get round trip", fmt.Sprintf("set err=%v get=%q err=%v", err, v, gerr))
		_ = c.Delete(ctx, k)
		_ = c.Close()
	}
}

func registry(ctx context.Context, dir string) {
	fmt.Println("registry")
	names := cache.Backends()
	check(strings.Join(names, ",") == "inmemory,memcached,redis,rediss,valkey,valkeys",
		"Backends() lists every registered scheme, sorted", strings.Join(names, ","))

	_, err := cache.Open(ctx, "nosuch:")
	check(err != nil && strings.Contains(err.Error(), "unknown backend") && strings.Contains(err.Error(), "inmemory"),
		`Open("nosuch:") names the registered backends`, fmt.Sprint(err))

	_, err = cache.FromConfig(ctx, cache.Config{Driver: "redis", URL: "valkey://127.0.0.1:1"})
	check(err != nil && strings.Contains(err.Error(), "does not match"),
		"FromConfig rejects driver/url mismatch", fmt.Sprint(err))

	c, err := cache.FromConfig(ctx, cache.Config{})
	check(err == nil, "FromConfig(empty Config) opens the default inmemory backend", fmt.Sprint(err))
	if c != nil {
		_ = c.Close()
	}

	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte("driver: inmemory\ndrivr: typo\n"), 0o600)
	_, err = cache.ReadConfig(bad)
	check(err != nil && !strings.Contains(err.Error(), "typo"), "ReadConfig rejects an unknown key without echoing the file", fmt.Sprint(err))

	if err := os.Setenv(cache.EnvBackend, "inmemory"); err != nil {
		check(false, "set CACHE_BACKEND for the FromEnv check", err.Error())
		return
	}
	_ = os.Unsetenv(cache.EnvURL)
	c, err = cache.FromEnv(ctx)
	check(err == nil, "FromEnv with CACHE_BACKEND=inmemory", fmt.Sprint(err))
	if c != nil {
		_ = c.Close()
	}
	_ = os.Unsetenv(cache.EnvBackend)
}

func drive(ctx context.Context, t target, dir, run string) {
	fmt.Printf("backend %s\n", t.name)
	c, cfg, err := open(ctx, t, dir)
	if err != nil {
		check(false, "open from config file", err.Error())
		return
	}
	defer c.Close()
	p := "verify-" + run + ":" + t.name + ":"
	var written []string
	defer func() {
		for _, k := range written {
			_ = c.Delete(ctx, k)
		}
	}()
	set := func(k string, v []byte, ttl time.Duration) error {
		written = append(written, p+k)
		return c.Set(ctx, p+k, v, ttl)
	}

	check(cfg.DriverName() == t.name, "Config.DriverName() names the driver", cfg.DriverName())
	perr := c.Ping(ctx)
	check(perr == nil, "Ping", fmt.Sprint(perr))

	v, err := c.Get(ctx, p+"missing")
	check(errors.Is(err, cache.ErrNotFound) && v == nil, "Get(missing)", fmt.Sprintf("value=%q err=%v", v, err))

	err = set("a", []byte("hello"), time.Minute)
	v, gerr := c.Get(ctx, p+"a")
	check(err == nil && gerr == nil && string(v) == "hello", "Set then Get", fmt.Sprintf("set err=%v, got %q", err, v))

	_ = set("a", []byte("world"), time.Minute)
	v, _ = c.Get(ctx, p+"a")
	check(string(v) == "world", "Set again overwrites", fmt.Sprintf("got %q", v))

	bin := []byte{0, 1, 2, 0xff, 0xfe, 0, '\n', '\r', ' '}
	_ = set("bin", bin, 0)
	v, _ = c.Get(ctx, p+"bin")
	check(bytes.Equal(v, bin), "binary value round trips (ttl 0 = no expiry)", fmt.Sprintf("got %v", v))

	v, _ = c.Get(ctx, p+"a")
	for i := range v {
		v[i] = 'X'
	}
	v2, _ := c.Get(ctx, p+"a")
	check(string(v2) == "world", "modifying a returned slice does not change the cached value", fmt.Sprintf("got %q", v2))

	err = c.Delete(ctx, p+"a")
	_, gerr = c.Get(ctx, p+"a")
	check(err == nil && errors.Is(gerr, cache.ErrNotFound), "Delete then Get", fmt.Sprintf("delete err=%v, get err=%v", err, gerr))
	err = c.Delete(ctx, p+"a")
	check(err == nil, "Delete of a missing key is not an error", fmt.Sprint(err))

	_ = set("with:colon/slash-1.2", []byte("k"), time.Minute)
	v, _ = c.Get(ctx, p+"with:colon/slash-1.2")
	check(string(v) == "k", "key with : / - . round trips", fmt.Sprintf("got %q", v))

	err = c.Set(ctx, "", []byte("e"), time.Minute)
	if t.emptyKeyOK {
		_, gerr = c.Get(ctx, "")
		check(err == nil && gerr == nil, `Set("") and Get("") work`, fmt.Sprintf("set err=%v get err=%v", err, gerr))
		_ = c.Delete(ctx, "")
	} else {
		_, gerr = c.Get(ctx, "")
		check(err != nil && gerr != nil && !errors.Is(gerr, cache.ErrNotFound),
			`Set("") and Get("") return an error that is not ErrNotFound`, fmt.Sprintf("set err=%v get err=%v", err, gerr))
	}

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	done := make(chan struct{})
	go func() { _, _ = c.Get(cctx, p+"a"); close(done) }()
	select {
	case <-done:
		check(true, "Get with a cancelled context returns promptly", "returned")
	case <-time.After(5 * time.Second):
		check(false, "Get with a cancelled context returns promptly", "still blocked after 5s")
	}

	_ = set("ttl", []byte("soon"), 2*time.Second)
	v, gerr = c.Get(ctx, p+"ttl")
	check(gerr == nil && string(v) == "soon", "Get right after Set with ttl 2s", fmt.Sprintf("got %q err=%v", v, gerr))
	time.Sleep(3200 * time.Millisecond)
	_, gerr = c.Get(ctx, p+"ttl")
	check(errors.Is(gerr, cache.ErrNotFound), "Get after the ttl has passed", fmt.Sprint(gerr))
}
