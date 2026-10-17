package secrettpl

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

const (
	maxTemplateBytes = 16 * 1024
	maxValueBytes    = 16 * 1024
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Endpoint is one MySQL member exposed to templates.
type Endpoint struct {
	Name string
	Host string
	Port int32
	// Index is the 1-based position in spec.members.
	Index int
}

// Data is the only object a tenant template can read.
type Data struct {
	Username         string
	Password         string
	PreviousPassword string
	Hosts            []string
	Plugin           string
	Cluster          string
	Master           Endpoint
	// Replica is one member that is not the selected master.
	// It is the first such member in spec.members order.
	Replica   Endpoint
	Members   []Endpoint
	Databases []string
	Item      Endpoint
}

// Entry expands a key and value for each member.
type Entry struct {
	ForEach string
	Key     string
	Value   string
}

// Render executes static templates and per-member entries.
// PASSWORD and PASSWORD_PREVIOUS are reserved and filled by the caller.
func Render(data map[string]string, entries []Entry, ctx Data) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range data {
		if err := validateTemplateSize(key, value); err != nil {
			return nil, err
		}
		renderedKey, err := renderString(key, ctx)
		if err != nil {
			return nil, fmt.Errorf("render key %q: %w", key, err)
		}
		renderedValue, err := renderString(value, ctx)
		if err != nil {
			return nil, fmt.Errorf("render value %q: %w", key, err)
		}
		if err := put(out, renderedKey, renderedValue); err != nil {
			return nil, err
		}
	}
	for _, entry := range entries {
		if entry.ForEach != "members" {
			return nil, fmt.Errorf("unsupported forEach %q", entry.ForEach)
		}
		if err := validateTemplateSize(entry.Key, entry.Value); err != nil {
			return nil, err
		}
		for _, member := range ctx.Members {
			itemCtx := ctx
			itemCtx.Item = member
			renderedKey, err := renderString(entry.Key, itemCtx)
			if err != nil {
				return nil, fmt.Errorf("render entry key: %w", err)
			}
			renderedValue, err := renderString(entry.Value, itemCtx)
			if err != nil {
				return nil, fmt.Errorf("render entry value: %w", err)
			}
			if err := put(out, renderedKey, renderedValue); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func put(out map[string]string, key, value string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("invalid secret key %q", key)
	}
	if len(value) > maxValueBytes {
		return fmt.Errorf("secret value %q exceeds %d bytes", key, maxValueBytes)
	}
	if _, exists := out[key]; exists {
		return fmt.Errorf("duplicate secret key %q", key)
	}
	if value == "" {
		return nil
	}
	out[key] = value
	return nil
}

func validateTemplateSize(key, value string) error {
	if len(key) > maxTemplateBytes || len(value) > maxTemplateBytes {
		return fmt.Errorf("template exceeds %d bytes", maxTemplateBytes)
	}
	return nil
}

func renderString(src string, data Data) (string, error) {
	funcs := sprig.TxtFuncMap()
	delete(funcs, "env")
	delete(funcs, "expandenv")
	delete(funcs, "getHostByName")
	tmpl, err := template.New("secret").Option("missingkey=error").Funcs(funcs).Parse(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}
