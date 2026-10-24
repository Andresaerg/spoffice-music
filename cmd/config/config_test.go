package config

import (
	"os"
	"testing"
)

func TestInvidiousHostDefault(t *testing.T) {
	orig, exists := os.LookupEnv("INVIDIOUS_HOST")
	defer func() {
		if exists {
			os.Setenv("INVIDIOUS_HOST", orig)
		} else {
			os.Unsetenv("INVIDIOUS_HOST")
		}
	}()

	os.Unsetenv("INVIDIOUS_HOST")
	if got := InvidiousHost(); got != DefaultInvidiousHost {
		t.Errorf("InvidiousHost() = %q; want %q", got, DefaultInvidiousHost)
	}
}

func TestInvidiousHostCustom(t *testing.T) {
	orig, exists := os.LookupEnv("INVIDIOUS_HOST")
	defer func() {
		if exists {
			os.Setenv("INVIDIOUS_HOST", orig)
		} else {
			os.Unsetenv("INVIDIOUS_HOST")
		}
	}()

	custom := "https://custom-invidious.example.com"
	os.Setenv("INVIDIOUS_HOST", custom)
	if got := InvidiousHost(); got != custom {
		t.Errorf("InvidiousHost() = %q; want %q", got, custom)
	}
}
