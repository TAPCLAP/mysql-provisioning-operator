package password

import (
	"testing"
	"time"
)

const currentPassword = "current-password"

func TestPlanGeneratesInitialPassword(t *testing.T) {
	out, err := Plan(Input{Now: time.Now(), SpecGeneration: 1, Retention: 24 * time.Hour, ServerSupportsRetain: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Password == "" || out.Retain || !out.ApplyPassword || !out.WriteSecret {
		t.Fatalf("%#v", out)
	}
}

func TestPlanRotatesAndRetains(t *testing.T) {
	now := time.Now()
	current := currentPassword
	out, err := Plan(Input{
		Now: now, SpecGeneration: 2, StatusGeneration: 1,
		Retention: 24 * time.Hour, SecretPassword: current, AppliedHash: Hash(current),
		RotatedAt: now.Add(-time.Hour), ServerSupportsRetain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Previous != current || out.Password == current || !out.Retain {
		t.Fatalf("%#v", out)
	}
	if out.RetainedUntil.Sub(now) != 24*time.Hour {
		t.Fatalf("retention = %s", out.RetainedUntil.Sub(now))
	}
}

func TestPlanBlocksWhileRetentionOpen(t *testing.T) {
	now := time.Now()
	current := currentPassword
	out, err := Plan(Input{
		Now: now, SpecGeneration: 2, StatusGeneration: 1,
		Retention: 5 * time.Minute, SecretPassword: current, AppliedHash: Hash(current),
		RetainedUntil: now.Add(time.Minute), ServerSupportsRetain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Blocked || out.ApplyPassword || out.Password != current {
		t.Fatalf("%#v", out)
	}
}

func TestPlanRefusesInstantReplace(t *testing.T) {
	now := time.Now()
	current := currentPassword
	out, err := Plan(Input{
		Now: now, SpecGeneration: 2, StatusGeneration: 1,
		Retention: 5 * time.Minute, SecretPassword: current, AppliedHash: Hash(current),
		ServerSupportsRetain: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Unsupported || out.ApplyPassword || out.Password != current {
		t.Fatalf("%#v", out)
	}
}

func TestPlanDiscardsExpiredPassword(t *testing.T) {
	now := time.Now()
	current := currentPassword
	out, err := Plan(Input{
		Now: now, SpecGeneration: 1, StatusGeneration: 1,
		SecretPassword: current, AppliedHash: Hash(current),
		RetainedUntil: now.Add(-time.Second), ServerSupportsRetain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.DiscardOld || !out.RetainedUntil.IsZero() {
		t.Fatalf("%#v", out)
	}
}
