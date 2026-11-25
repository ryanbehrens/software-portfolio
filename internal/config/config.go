// Package config loads the list of apps shown on the portfolio.
//
// Apps live in apps.json. To add a new app, add an entry there and redeploy.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// App is one project on the portfolio.
type App struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Tagline     string   `json:"tagline"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	URL         string   `json:"url"`
	Source      string   `json:"source,omitempty"`
	Image       string   `json:"image,omitempty"`
	Stack       []string `json:"stack"`
	Badge       string   `json:"badge,omitempty"`
	Featured    bool     `json:"featured,omitempty"`
	// Monitor turns uptime checks on or off. It defaults to true; set it to
	// false for links that aren't ours to check, such as an app store page.
	Monitor *bool `json:"monitor,omitempty"`
}

// Monitored reports whether the status checker should ping this app.
func (a App) Monitored() bool { return a.Monitor == nil || *a.Monitor }

type file struct {
	Apps []App `json:"apps"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Parse decodes and validates apps.json. Unknown fields are rejected so a
// typo in a key name fails loudly instead of being silently ignored.
func Parse(data []byte) ([]App, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("apps.json: %w", err)
	}
	if len(f.Apps) == 0 {
		return nil, errors.New("apps.json: no apps listed")
	}

	seen := make(map[string]bool, len(f.Apps))
	var problems []string
	for i, a := range f.Apps {
		where := fmt.Sprintf("apps[%d]", i)
		if a.ID != "" {
			where = fmt.Sprintf("app %q", a.ID)
		}
		switch {
		case !idPattern.MatchString(a.ID):
			problems = append(problems, where+": id must be lowercase letters, digits and dashes")
		case seen[a.ID]:
			problems = append(problems, where+": duplicate id")
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Name) == "" {
			problems = append(problems, where+": name is required")
		}
		if err := checkURL(a.URL); err != nil {
			problems = append(problems, where+": url "+err.Error())
		}
		if a.Source != "" {
			if err := checkURL(a.Source); err != nil {
				problems = append(problems, where+": source "+err.Error())
			}
		}
	}
	if len(problems) > 0 {
		return nil, errors.New("apps.json:\n  " + strings.Join(problems, "\n  "))
	}
	return f.Apps, nil
}

func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("%q is not a valid http(s) address", raw)
	}
	return nil
}
