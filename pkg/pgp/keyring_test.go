package pgp

import (
	"math"
	"testing"
	"time"
)

func TestGenConfigClampsLifetime(t *testing.T) {
	// > ~136 Jahre überläuft uint32-Sekunden – muss geklemmt werden.
	cfg := genConfig(200 * 365 * 24 * time.Hour)
	if cfg.KeyLifetimeSecs != math.MaxUint32 {
		t.Fatalf("Lifetime nicht geklemmt: %d", cfg.KeyLifetimeSecs)
	}
	cfg = genConfig(2 * 365 * 24 * time.Hour)
	if want := uint32(2 * 365 * 24 * 3600); cfg.KeyLifetimeSecs != want {
		t.Fatalf("normale Lifetime verändert: %d != %d", cfg.KeyLifetimeSecs, want)
	}
	if genConfig(0).KeyLifetimeSecs != 0 {
		t.Fatal("0 = kein Ablauf muss erhalten bleiben")
	}
}
