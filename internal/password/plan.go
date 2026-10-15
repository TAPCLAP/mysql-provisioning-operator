package password

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	// DefaultRetention is the window used when spec omits retention.
	DefaultRetention = 24 * time.Hour
	// MinRetention is the shortest window the API accepts.
	MinRetention = 5 * time.Minute
)

// Input is the rotation state the planner can see. It never logs passwords.
type Input struct {
	Now                  time.Time
	SpecGeneration       int32
	StatusGeneration     int32
	Retention            time.Duration
	RotateEvery          time.Duration
	RotatedAt            time.Time
	SecretPassword       string
	AppliedHash          string
	RetainedUntil        time.Time
	ServerSupportsRetain bool
}

// Output tells the controller which password to publish and which SQL to run.
type Output struct {
	Password         string
	Previous         string
	WriteSecret      bool
	ApplyPassword    bool
	Retain           bool
	DiscardOld       bool
	Blocked          bool
	Unsupported      bool
	StatusGeneration int32
	RotatedAt        time.Time
	RetainedUntil    time.Time
	Hash             string
}

// Plan decides whether to generate, adopt, rotate, or discard a password.
func Plan(in Input) (Output, error) {
	retention := in.Retention
	if retention == 0 {
		retention = DefaultRetention
	}
	out := Output{
		Password:         in.SecretPassword,
		StatusGeneration: in.StatusGeneration,
		RotatedAt:        in.RotatedAt,
		RetainedUntil:    in.RetainedUntil,
	}
	windowOpen := !in.RetainedUntil.IsZero() && in.Now.Before(in.RetainedUntil)
	if !in.RetainedUntil.IsZero() && !in.Now.Before(in.RetainedUntil) {
		out.DiscardOld = true
		out.RetainedUntil = time.Time{}
		windowOpen = false
	}

	if in.SecretPassword == "" {
		generated, err := Generate()
		if err != nil {
			return Output{}, err
		}
		out.Password = generated
		out.WriteSecret = true
		out.ApplyPassword = true
		out.Hash = Hash(generated)
		out.RotatedAt = in.Now
		out.StatusGeneration = in.SpecGeneration
		return out, nil
	}

	if in.AppliedHash == "" {
		out.ApplyPassword = true
		out.Hash = Hash(in.SecretPassword)
		out.RotatedAt = in.Now
		out.StatusGeneration = in.SpecGeneration
		return out, nil
	}

	scheduleDue := in.RotateEvery > 0 && !in.RotatedAt.IsZero() && !in.Now.Before(in.RotatedAt.Add(in.RotateEvery))
	generationDue := in.SpecGeneration > in.StatusGeneration
	externalEdit := Hash(in.SecretPassword) != in.AppliedHash
	if !scheduleDue && !generationDue && !externalEdit {
		out.Hash = in.AppliedHash
		return out, nil
	}
	if windowOpen {
		out.Blocked = true
		out.Hash = in.AppliedHash
		return out, nil
	}
	if !in.ServerSupportsRetain {
		out.Unsupported = true
		out.Hash = in.AppliedHash
		return out, nil
	}

	out.ApplyPassword = true
	out.Retain = true
	out.WriteSecret = true
	out.RetainedUntil = in.Now.Add(retention)
	out.RotatedAt = in.Now
	out.StatusGeneration = in.SpecGeneration
	if generationDue || scheduleDue {
		if Hash(in.SecretPassword) == in.AppliedHash {
			out.Previous = in.SecretPassword
		}
		generated, err := Generate()
		if err != nil {
			return Output{}, err
		}
		out.Password = generated
	}
	out.Hash = Hash(out.Password)
	return out, nil
}

// Generate returns a URL-safe password from crypto/rand.
func Generate() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Hash is the SHA-256 hex digest stored in status. It is not the MySQL hash.
func Hash(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}
