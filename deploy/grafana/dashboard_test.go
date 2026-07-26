package grafana

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type dashboard struct {
	UID        string `json:"uid"`
	Refresh    string `json:"refresh"`
	Panels     []panel
	Templating struct {
		List []struct {
			Name string `json:"name"`
		} `json:"list"`
	} `json:"templating"`
}

type panel struct {
	ID         int    `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Collapsed  bool   `json:"collapsed"`
	GridPos    grid   `json:"gridPos"`
	Datasource struct {
		UID string `json:"uid"`
	} `json:"datasource"`
	Targets []struct {
		Expr    string `json:"expr"`
		Instant bool   `json:"instant"`
	} `json:"targets"`
}

type grid struct {
	H int `json:"h"`
	W int `json:"w"`
	X int `json:"x"`
	Y int `json:"y"`
}

func TestDashboardContract(t *testing.T) {
	raw, err := os.ReadFile("dashboards/openclaw-overview.json")
	if err != nil {
		t.Fatal(err)
	}
	var d dashboard
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("invalid dashboard JSON: %v", err)
	}
	if d.UID != "openclaw-observatory" {
		t.Fatalf("unexpected dashboard UID %q", d.UID)
	}
	if d.Refresh != "30s" {
		t.Fatalf("dashboard refresh must stay above the 15s scrape interval, got %q", d.Refresh)
	}

	ids := map[int]string{}
	for i, p := range d.Panels {
		if prior, exists := ids[p.ID]; exists {
			t.Fatalf("duplicate panel id %d: %q and %q", p.ID, prior, p.Title)
		}
		ids[p.ID] = p.Title
		if p.GridPos.W <= 0 || p.GridPos.H <= 0 || p.GridPos.X < 0 || p.GridPos.X+p.GridPos.W > 24 {
			t.Fatalf("panel %d %q has invalid grid position: %#v", p.ID, p.Title, p.GridPos)
		}
		if p.Type == "row" {
			if p.Collapsed {
				t.Fatalf("row %d %q must not collapse panels that are stored at dashboard root", p.ID, p.Title)
			}
			continue
		}
		if len(p.Targets) == 0 {
			t.Fatalf("panel %d %q has no targets", p.ID, p.Title)
		}
		if p.Datasource.UID != "$datasource" {
			t.Fatalf("panel %d %q bypasses the remote datasource variable with UID %q", p.ID, p.Title, p.Datasource.UID)
		}
		for _, target := range p.Targets {
			if strings.TrimSpace(target.Expr) == "" {
				t.Fatalf("panel %d %q has an empty PromQL expression", p.ID, p.Title)
			}
			if strings.Contains(target.Expr, "[5m]") {
				t.Fatalf("panel %d %q hard-codes a 5m rate window", p.ID, p.Title)
			}
			if p.Type == "stat" && !target.Instant {
				t.Fatalf("stat panel %d %q must use an instant query", p.ID, p.Title)
			}
		}
		for j := 0; j < i; j++ {
			other := d.Panels[j]
			if overlaps(p.GridPos, other.GridPos) {
				t.Fatalf("panels overlap: %d %q and %d %q", p.ID, p.Title, other.ID, other.Title)
			}
		}
	}

	variables := map[string]bool{}
	for _, variable := range d.Templating.List {
		variables[variable.Name] = true
	}
	for _, name := range []string{"datasource", "instance", "agent", "model"} {
		if !variables[name] {
			t.Fatalf("missing bounded dashboard variable %q", name)
		}
	}
	text := string(raw)
	for _, metric := range []string{
		"openclaw_llm_tokens_by_agent_model_total",
		"openclaw_llm_cost_usd_by_agent_model_total",
	} {
		if !strings.Contains(text, metric) {
			t.Fatalf("dashboard does not use canonical metric %q", metric)
		}
	}
}

func overlaps(a, b grid) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}
