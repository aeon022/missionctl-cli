package cmd

import (
	"fmt"
	"os/exec"
)

// wealthStatus adds budgetctl's anchored account balances to investctl's portfolio value. Accounts
// without an opening balance are never estimated — they are reported as missing, and currencies other
// than EUR are listed, not converted. An empty text means "nothing to show" and the row is skipped.
func wealthStatus() cardStatus {
	var bal struct {
		Data []struct {
			Anchored bool    `json:"anchored"`
			Balance  float64 `json:"balance"`
		} `json:"data"`
	}
	cash, anchored, missing := 0.0, 0, 0
	if _, err := exec.LookPath("budgetctl"); err == nil && runToolJSON("budgetctl", []string{"balance", "--json"}, &bal) {
		for _, a := range bal.Data {
			if a.Anchored {
				cash += a.Balance
				anchored++
			} else {
				missing++
			}
		}
	}
	var inv struct {
		Data struct {
			Totals []struct {
				Currency string
				Value    float64
			}
		} `json:"data"`
	}
	invested, other := 0.0, ""
	hasInvest := false
	if _, err := exec.LookPath("investctl"); err == nil && runToolJSON("investctl", []string{"summary", "--json"}, &inv) {
		for _, t := range inv.Data.Totals {
			if t.Currency == "EUR" || t.Currency == "" {
				invested += t.Value
				hasInvest = true
			} else {
				other += fmt.Sprintf(" + %.0f %s invested (not converted)", t.Value, t.Currency)
			}
		}
	}
	if anchored == 0 && !hasInvest && other == "" {
		return cardStatus{}
	}
	text := fmt.Sprintf("€%.0f net worth\n€%.0f cash + €%.0f invested", cash+invested, cash, invested)
	if other != "" {
		text += other
	}
	if missing > 0 {
		text += fmt.Sprintf("\n%d account(s) without opening balance not counted", missing)
	}
	return cardStatus{text: text}
}
