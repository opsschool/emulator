package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := parse(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.LogLevel != "info" || c.DBPoolSize != 20 || c.CacheTTL != time.Minute || !c.ClientKeepAlive {
		t.Errorf("unexpected defaults: %+v", c)
	}
}

func TestOverridesAndErrors(t *testing.T) {
	c, err := parse(env(map[string]string{"SHOP_LOG_LEVEL": "debug", "SHOP_DB_POOL_SIZE": " 5 ", "SHOP_CLIENT_KEEPALIVE": "false"}))
	if err != nil || c.LogLevel != "debug" || c.DBPoolSize != 5 || c.ClientKeepAlive {
		t.Fatalf("got %+v, %v", c, err)
	}
	_, err = parse(env(map[string]string{"SHOP_LOG_LEVEL": "verbose", "SHOP_CACHE_TTL": "60", "SHOP_DB_POOL_SIZE": "lots"}))
	for _, want := range []string{"SHOP_LOG_LEVEL", "SHOP_CACHE_TTL", "SHOP_DB_POOL_SIZE"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}
