package cmd

import (
	"strings"
	"testing"
)

func TestWealthStatus(t *testing.T) {
	bal := `{"data":[{"account":"giro","anchored":true,"balance":2949.9},{"account":"alt","anchored":false,"balance":0}],"currency":"EUR"}`
	inv := `{"data":{"Totals":[{"Currency":"EUR","Value":9395.4},{"Currency":"USD","Value":1200}]}}`

	stubTools(t, map[string]string{"budgetctl": bal, "investctl": inv})
	got := wealthStatus().text
	for _, want := range []string{"€12345 net worth", "€2950 cash + €9395 invested", "+ 1200 USD invested (not converted)", "1 account(s) without opening balance not counted"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}

	// no anchored account and no holdings → nothing to show (an unanchored sum must never be shown)
	stubTools(t, map[string]string{"budgetctl": `{"data":[{"account":"giro","anchored":false,"balance":0}]}`, "investctl": `{"data":{"Totals":[]}}`})
	if got := wealthStatus().text; got != "" {
		t.Errorf("want empty, got %q", got)
	}

	// tools missing or failing → empty, no panic
	stubTools(t, map[string]string{"budgetctl": "", "investctl": ""})
	if got := wealthStatus().text; got != "" {
		t.Errorf("failing tools: %q", got)
	}
}
