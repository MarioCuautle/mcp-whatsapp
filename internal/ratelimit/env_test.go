package ratelimit

import "testing"

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestConfigFromEnvDefaults(t *testing.T) {
	cfg, changed, err := ConfigFromEnv(envOf(nil))
	if err != nil || changed || cfg != DefaultConfig() {
		t.Fatalf("expected untouched defaults, got %+v changed=%v err=%v", cfg, changed, err)
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	cfg, changed, err := ConfigFromEnv(envOf(map[string]string{
		EnvMinSendInterval:      "20s",
		EnvMaxSendsPerHour:      "60",
		EnvNonContactMaxPerHour: "10",
	}))
	if err != nil || !changed {
		t.Fatalf("unexpected: changed=%v err=%v", changed, err)
	}
	want := DefaultConfig()
	want.MinSendInterval, want.MaxSendsPerHour, want.NonContactMaxPerHour = 20e9, 60, 10
	if cfg != want {
		t.Fatalf("got %+v want %+v", cfg, want)
	}
}

func TestConfigFromEnvRejectsBadValues(t *testing.T) {
	for name, v := range map[string]string{
		EnvMinSendInterval:       "abc",
		EnvNonContactMinInterval: "-5s",
		EnvMaxSendsPerHour:       "0",
		EnvNonContactMaxPerHour:  "ten",
		EnvMaxUsyncPerMinute:     "-1",
		EnvMinStatusInterval:     "0s",
		EnvMaxStatusesPerDay:     "many",
	} {
		if _, _, err := ConfigFromEnv(envOf(map[string]string{name: v})); err == nil {
			t.Errorf("%s=%q should be rejected", name, v)
		}
	}
}

func TestConfigFromEnvStatusOverrides(t *testing.T) {
	cfg, changed, err := ConfigFromEnv(envOf(map[string]string{
		EnvMinStatusInterval: "2m",
		EnvMaxStatusesPerDay: "20",
	}))
	if err != nil || !changed {
		t.Fatalf("unexpected: changed=%v err=%v", changed, err)
	}
	if cfg.MinStatusInterval != 2*60e9 || cfg.MaxStatusesPerDay != 20 {
		t.Fatalf("got %+v", cfg)
	}
}
