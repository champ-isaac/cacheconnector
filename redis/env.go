package redis

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func OptionsFromEnv() ([]Option, error) { return OptionsFromEnvPrefix("REDIS") }

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

func FromEnv(ctx context.Context) (*Client, error) {
	opts, err := OptionsFromEnv()
	if err != nil {
		return nil, err
	}
	return New(ctx, opts...)
}
