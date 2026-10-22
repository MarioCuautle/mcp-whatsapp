package ratelimit

import (
	"fmt"
	"strconv"
	"time"
)

// Environment variables that override the DefaultConfig limits. Unset or empty
// keeps the default; an invalid or non-positive value is an error so a typo can
// never silently disable a guard.
const (
	EnvMinSendInterval       = "WHATSAPP_MCP_RATE_MIN_SEND_INTERVAL"       // Go duration, e.g. "20s"
	EnvMaxSendsPerHour       = "WHATSAPP_MCP_RATE_MAX_SENDS_PER_HOUR"      // integer
	EnvNonContactMinInterval = "WHATSAPP_MCP_RATE_NONCONTACT_MIN_INTERVAL" // Go duration
	EnvNonContactMaxPerHour  = "WHATSAPP_MCP_RATE_NONCONTACT_MAX_PER_HOUR" // integer
	EnvMaxUsyncPerMinute     = "WHATSAPP_MCP_RATE_MAX_USYNC_PER_MINUTE"    // integer
)

// ConfigFromEnv returns DefaultConfig with any limits overridden from getenv
// (pass os.Getenv). The second result reports whether anything was overridden.
func ConfigFromEnv(getenv func(string) string) (Config, bool, error) {
	cfg := DefaultConfig()
	changed := false
	dur := func(name string, dst *time.Duration) error {
		v := getenv(name)
		if v == "" {
			return nil
		}
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return fmt.Errorf("ratelimit: %s=%q must be a positive duration such as \"30s\"", name, v)
		}
		*dst, changed = d, true
		return nil
	}
	num := func(name string, dst *int) error {
		v := getenv(name)
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("ratelimit: %s=%q must be a positive integer", name, v)
		}
		*dst, changed = n, true
		return nil
	}
	for _, err := range []error{
		dur(EnvMinSendInterval, &cfg.MinSendInterval),
		num(EnvMaxSendsPerHour, &cfg.MaxSendsPerHour),
		dur(EnvNonContactMinInterval, &cfg.NonContactMinInterval),
		num(EnvNonContactMaxPerHour, &cfg.NonContactMaxPerHour),
		num(EnvMaxUsyncPerMinute, &cfg.MaxUsyncPerMinute),
	} {
		if err != nil {
			return Config{}, false, err
		}
	}
	return cfg, changed, nil
}
