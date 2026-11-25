package config

import (
	"os"
	"strings"
	"testing"
)

func TestParseRealAppsFile(t *testing.T) {
	data, err := os.ReadFile("../../apps.json")
	if err != nil {
		t.Fatal(err)
	}
	apps, err := Parse(data)
	if err != nil {
		t.Fatalf("apps.json should be valid: %v", err)
	}
	for _, a := range apps {
		if a.Image != "" {
			if _, err := os.Stat("../../web/" + a.Image); err != nil {
				t.Errorf("app %q: image %s is missing", a.ID, a.Image)
			}
		}
	}
}

func TestParseRejectsBadEntries(t *testing.T) {
	tests := []struct {
		name, json, want string
	}{
		{"empty list", `{"apps":[]}`, "no apps"},
		{"unknown field", `{"apps":[{"id":"a","name":"A","url":"https://a.com","colour":"red"}]}`, "unknown field"},
		{"bad id", `{"apps":[{"id":"My App","name":"A","url":"https://a.com"}]}`, "id must be"},
		{"duplicate id", `{"apps":[{"id":"a","name":"A","url":"https://a.com"},{"id":"a","name":"B","url":"https://b.com"}]}`, "duplicate id"},
		{"missing name", `{"apps":[{"id":"a","url":"https://a.com"}]}`, "name is required"},
		{"bad url", `{"apps":[{"id":"a","name":"A","url":"ftp://a.com"}]}`, "url"},
		{"bad source", `{"apps":[{"id":"a","name":"A","url":"https://a.com","source":"github"}]}`, "source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.json))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got error %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestMonitoredDefaultsToTrue(t *testing.T) {
	off := false
	if !(App{}).Monitored() {
		t.Error("apps should be monitored unless turned off")
	}
	if (App{Monitor: &off}).Monitored() {
		t.Error(`"monitor": false should turn checks off`)
	}
}
