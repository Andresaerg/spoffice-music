package config

import (
	"cmp"
	"os"
)

const DefaultInvidiousHost = "https://invidious.f5.si"

// InvidiousHost returns the configured Invidious instance host URL,
// falling back to DefaultInvidiousHost if INVIDIOUS_HOST is unset.
func InvidiousHost() string {
	return cmp.Or(os.Getenv("INVIDIOUS_HOST"), DefaultInvidiousHost)
}
