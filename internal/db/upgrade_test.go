package db

import (
	"path/filepath"
	"testing"

	"maintenance-reminder/internal/template"
)

// TestOpenUpgradesTheLegacyStrategies checks that a database of the first
// release is moved to the messages of the current release, so that an
// existing install sends the grouped markdown messages without an edit of
// every strategy.
func TestOpenUpgradesTheLegacyStrategies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// The rows that the first release wrote are put back, as if the
	// database were older than this build.
	for kind, bodies := range legacyStrategyBodies {
		if _, err = database.Exec(`UPDATE strategies SET template=?,title_template='',item_template='',format='txt' WHERE type=?`, bodies[0], kind); err != nil {
			t.Fatal(err)
		}
	}
	if err = upgradeLegacyStrategies(database); err != nil {
		t.Fatal(err)
	}
	for kind := range legacyStrategyBodies {
		var format, body, title string
		if err = database.QueryRow(`SELECT format,template,title_template FROM strategies WHERE type=?`, kind).Scan(&format, &body, &title); err != nil {
			t.Fatal(err)
		}
		if format != template.FormatMarkdown {
			t.Errorf("%s still has the format %q", kind, format)
		}
		if body != template.DefaultBodyFor(template.FormatMarkdown, kind) {
			t.Errorf("%s did not get the markdown preset: %q", kind, body)
		}
		if title == "" {
			t.Errorf("%s got no title", kind)
		}
	}
	// The summary is laid out by the item line of the current preset.
	var item string
	if err = database.QueryRow(`SELECT item_template FROM strategies WHERE type=?`, template.KindMonthly).Scan(&item); err != nil {
		t.Fatal(err)
	}
	if item != template.DefaultItemFor(template.FormatMarkdown) {
		t.Errorf("the monthly summary did not get the item preset: %q", item)
	}
}

// TestUpgradeKeepsTheTextOfTheUser checks that a message the user edited is
// left alone, because that text belongs to them.
func TestUpgradeKeepsTheTextOfTheUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	own := "## 我自己写的\n\n项目：{{project}}"
	if _, err = database.Exec(`UPDATE strategies SET template=?,format='markdown' WHERE type='milestone'`, own); err != nil {
		t.Fatal(err)
	}
	if err = upgradeLegacyStrategies(database); err != nil {
		t.Fatal(err)
	}
	var format, body string
	if err = database.QueryRow(`SELECT format,template FROM strategies WHERE type='milestone'`).Scan(&format, &body); err != nil {
		t.Fatal(err)
	}
	if format != template.FormatMarkdown || body != own {
		t.Errorf("an edited strategy was changed: %q %q", format, body)
	}
}

// TestOpenUpgradesOnlyOnce checks that a second start does not replace the
// text a user has kept.
func TestOpenUpgradesOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(`UPDATE strategies SET template=? WHERE type='milestone'`, legacyStrategyBodies[template.KindMilestone][0]); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var body string
	if err = database.QueryRow(`SELECT template FROM strategies WHERE type='milestone'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != template.DefaultBodyFor(template.FormatMarkdown, template.KindMilestone) {
		t.Errorf("the legacy text survived a restart: %q", body)
	}
	// A text the user writes after the upgrade is not the legacy text, so a
	// later start must not touch it.
	own := "自定义 {{project}}"
	if _, err = database.Exec(`UPDATE strategies SET template=? WHERE type='milestone'`, own); err != nil {
		t.Fatal(err)
	}
	if err = upgradeLegacyStrategies(database); err != nil {
		t.Fatal(err)
	}
	if err = database.QueryRow(`SELECT template FROM strategies WHERE type='milestone'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != own {
		t.Errorf("the text of the user was replaced: %q", body)
	}
}
