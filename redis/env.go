package redis

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// OptionsFromEnv reads the connection settings from environment variables:
//
//	REDIS_ADDR      host:port (default 127.0.0.1:6379)
//	REDIS_USERNAME, REDIS_PASSWORD, REDIS_DB
//
// Values are trimmed. A variable that is unset or empty keeps the default of
// New (an empty REDIS_PASSWORD means no authentication). Only REDIS_DB can be
// rejected here, by name; no error contains the password.
func OptionsFromEnv() ([]Option, error) { return OptionsFromEnvPrefix("REDIS") }

// OptionsFromEnvPrefix is OptionsFromEnv with another variable prefix, for
// example "VALKEY" reads VALKEY_ADDR, VALKEY_USERNAME, VALKEY_PASSWORD and
// VALKEY_DB.
func OptionsFromEnvPrefix(prefix string) ([]Option, error) {
	get := func(k string) string { return strings.TrimSpace(os.Getenv(prefix + "_" + k)) }
	var opts []Option
	if addr := get("ADDR"); addr != "" {
		opts = append(opts, WithAddr(addr))
	}
	if user := get("USERNAME"); user != "" {
		opts = append(opts, WithUsername(user))
	}
	if pw := get("PASSWORD"); pw != "" {
		opts = append(opts, WithPassword(pw))
	}
	if s := get("DB"); s != "" {
		db, err := strconv.Atoi(s)
		if err != nil || db < 0 {
			return nil, fmt.Errorf("redis: bad %s_DB %q", prefix, s)
		}
		opts = append(opts, WithDB(db))
	}
	return opts, nil
}

// FromEnv connects a Redis client with the settings of OptionsFromEnv. Unlike
// cache.FromEnv it always uses Redis and ignores CACHE_BACKEND.
func FromEnv(ctx context.Context) (*Client, error) {
	opts, err := OptionsFromEnv()
	if err != nil {
		return nil, err
	}
	return New(ctx, opts...)
}
