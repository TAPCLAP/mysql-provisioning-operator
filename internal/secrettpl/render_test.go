package secrettpl

import "testing"

const (
	masterHost  = "db1-1.internal"
	replicaHost = "db1-2.internal"
)

func TestRenderTemplatesAndEntries(t *testing.T) {
	out, err := Render(map[string]string{
		"USERNAME": "{{ .Username }}",
		"DSN":      "{{ .Username }}:{{ .Password }}@tcp({{ .Master.Host }}:{{ .Master.Port }})/app",
		"REPLICA":  "{{ .Replica.Host }}",
		"HOSTS":    `{{ .Hosts | join "," }}`,
	}, []Entry{{
		ForEach: "members",
		Key:     "{{ .Item.Name | upper }}_HOST",
		Value:   "{{ .Item.Host }}",
	}, {
		ForEach: "members",
		Key:     "MEMBER_{{ .Item.Index }}_HOST",
		Value:   "{{ .Item.Host }}",
	}}, Data{
		Username: "payments_app",
		Password: "secret",
		Hosts:    []string{"%"},
		Master:   Endpoint{Name: "db1-1", Host: masterHost, Port: 3306},
		Replica:  Endpoint{Name: "db1-2", Host: replicaHost, Port: 3306, Index: 2},
		Members:  []Endpoint{{Name: "db1-1", Host: masterHost, Port: 3306, Index: 1}, {Name: "db1-2", Host: replicaHost, Port: 3306, Index: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["USERNAME"] != "payments_app" || out["DB1-1_HOST"] != masterHost || out["DB1-2_HOST"] != replicaHost || out["MEMBER_1_HOST"] != masterHost || out["MEMBER_2_HOST"] != replicaHost {
		t.Fatalf("%#v", out)
	}
	if out["HOSTS"] != "%" || out["DSN"] != "payments_app:secret@tcp("+masterHost+":3306)/app" || out["REPLICA"] != replicaHost {
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
