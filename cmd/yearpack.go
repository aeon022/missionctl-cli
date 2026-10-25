package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// The year pack gathers what a tax advisor asks for at year end — outgoing invoices, receipts and bank
// bookings — into one folder, by calling each tool's own export. Nothing is sent anywhere.
var yearpackCmd = &cobra.Command{
	Use:   "yearpack --out FOLDER [--year 2026]",
	Short: "Collect invoices, receipts and bookings of one year into a folder for your tax advisor",
	Long: `Runs the export of every installed tool for one calendar year:
  invoices/   outgoing-invoice ledger (CSV) and the PDFs        (invoicectl)
  receipts/   receipt list (CSV) and numbered copies             (receiptctl)
  bookings.csv, category-totals.csv                              (budgetctl)
and writes README.txt with what is in it and what still needs a look (unchecked receipts, receipts
without a booking, draft or overdue invoices). The folder must be new or empty. It contains your
financial records: keep it private and hand it over yourself.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, _ := cmd.Flags().GetString("out")
		year, _ := cmd.Flags().GetInt("year")
		if out == "" {
			return errors.New("--out FOLDER is required")
		}
		if year == 0 {
			year = time.Now().Year()
			if time.Now().Month() == time.January { // in January you usually pack the year that just ended
				year--
			}
		}
		return runYearpack(cmd.OutOrStdout(), out, year)
	},
}

func init() {
	yearpackCmd.Flags().String("out", "", "target folder (new or empty)")
	yearpackCmd.Flags().Int("year", 0, "calendar year (default: this year; in January: last year)")
	rootCmd.AddCommand(yearpackCmd)
}

type packPart struct {
	tool string
	what string
	args []string
}

func runYearpack(w io.Writer, dir string, year int) error {
	if es, err := os.ReadDir(dir); err == nil && len(es) > 0 {
		return fmt.Errorf("%s is not empty — choose a new folder", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	from, to := fmt.Sprintf("%d-01-01", year), fmt.Sprintf("%d-12-31", year)
	parts := []packPart{
		{"invoicectl", "outgoing invoices", []string{"export", "--from", from, "--to", to, "--out", filepath.Join(dir, "invoices")}},
		{"receiptctl", "receipts", []string{"export", "--from", from, "--to", to, "--out", filepath.Join(dir, "receipts")}},
		{"budgetctl", "bank bookings", []string{"export", "--year", fmt.Sprint(year), "--output", filepath.Join(dir, "bookings.csv")}},
		{"budgetctl", "category totals", []string{"export", "--year", fmt.Sprint(year), "--summary", "--output", filepath.Join(dir, "category-totals.csv")}},
	}
	var readme strings.Builder
	fmt.Fprintf(&readme, "Year pack %d — created %s\n\nBookkeeping aid, not tax advice. Check everything before you rely on it.\n\nIncluded:\n", year, time.Now().Format("2006-01-02"))
	failed := 0
	for _, p := range parts {
		switch {
		case !onPath(p.tool):
			fmt.Fprintf(&readme, "  - %s: %s not installed, skipped\n", p.what, p.tool)
			fmt.Fprintf(w, "  %-16s skipped (%s not installed)\n", p.what, p.tool)
		default:
			if b, err := exec.Command(p.tool, p.args...).CombinedOutput(); err != nil {
				failed++
				msg := strings.TrimSpace(string(b))
				if i := strings.LastIndex(msg, "\n"); i >= 0 {
					msg = msg[i+1:]
				}
				fmt.Fprintf(&readme, "  - %s: FAILED (%s)\n", p.what, msg)
				fmt.Fprintf(w, "  %-16s FAILED: %s\n", p.what, msg)
			} else {
				fmt.Fprintf(&readme, "  - %s: ok\n", p.what)
				fmt.Fprintf(w, "  %-16s ok\n", p.what)
			}
		}
	}
	readme.WriteString("\nStill to look at (all time, not only this year):\n")
	todo := 0
	if r := gatherReceipts(); r != nil {
		fmt.Fprintf(&readme, "  - receipts: %d unchecked, %d without a booking, %d unclear (of %d)\n", r.Unchecked, r.Unmatched, r.Unclear, r.Total)
		todo += r.Unchecked + r.Unmatched
	}
	if i := gatherInvoices(); i != nil {
		fmt.Fprintf(&readme, "  - invoices: %d open, %d overdue, %d draft(s)\n", i.Open, i.Overdue, i.Drafts)
		todo += i.Overdue + i.Drafts
	}
	if todo == 0 {
		readme.WriteString("  - nothing flagged\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte(readme.String()), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nWritten to %s (see README.txt for what still needs a look).\n", dir)
	if failed > 0 {
		return fmt.Errorf("%d part(s) failed — the pack is incomplete", failed)
	}
	return nil
}
