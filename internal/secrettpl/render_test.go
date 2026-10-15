package secrettpl

import "testing"

func TestRenderTemplatesAndEntries(t *testing.T) {
	out, err := Render(map[string]string{
		"USERNAME": "{{ .Username }}",
		"DSN":      "{{ .Username }}:{{ .Password }}@tcp({{ .Master.Host }}:{{ .Master.Port }})/app",
		"HOSTS":    `{{ .Hosts | join "," }}`,
	}, []Entry{{
		ForEach: "members",
		Key:     "{{ .Item.Name | upper }}_HOST",
		Value:   "{{ .Item.Host }}",
	}}, Data{
		Username: "payments_app",
		Password: "secret",
		Hosts:    []string{"%"},
		Master:   Endpoint{Name: "db1-1", Host: "db1-1.internal", Port: 3306},
		Members:  []Endpoint{{Name: "db1-1", Host: "db1-1.internal", Port: 3306}, {Name: "db1-2", Host: "db1-2.internal", Port: 3306}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["USERNAME"] != "payments_app" || out["DB1-1_HOST"] != "db1-1.internal" || out["DB1-2_HOST"] != "db1-2.internal" {
		t.Fatalf("%#v", out)
	}
	if out["HOSTS"] != "%" || out["DSN"] != "payments_app:secret@tcp(db1-1.internal:3306)/app" {
		t.Fatalf("%#v", out)
	}
}

func TestRenderRejectsEnv(t *testing.T) {
	if _, err := Render(map[string]string{"X": `{{ env "HOME" }}`}, nil, Data{}); err == nil {
		t.Fatal("env function should be unavailable")
	}
}

func TestRenderRejectsBadKey(t *testing.T) {
	if _, err := Render(map[string]string{"bad key": "value"}, nil, Data{}); err == nil {
		t.Fatal("expected invalid key")
	}
}
