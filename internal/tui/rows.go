package tui

import (
	"math/rand"
	"time"
)

// TableRow is one line of the card-3 table. The data is fake for now — a
// generator stands in until the real data source is wired in later.
type TableRow struct {
	Customer string
	Package  string
	Panel    string // "NBB" or "PACE"
	Status   string // "DONE", "PENDING", "ACTIVE"
}

// fakeRows builds n rows of plausible-looking billing data so the table
// layout can be developed before the real data source exists.
func fakeRows(n int) []TableRow {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	customers := []string{
		"hp_david_mcolony", "hp_ahmed_ravi", "hp_sara_khan",
		"hp_bilal_sheikh", "hp_usman_tariq", "hp_faisal_nadeem",
		"hp_zain_mehmood", "hp_ali_hassan",
	}
	packages := []string{"PKG5MB", "PKG10MB", "PKG20MB", "PKG50MB"}
	panels := []string{"NBB", "PACE"}
	statuses := []string{"DONE", "PENDING", "ACTIVE"}

	rows := make([]TableRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, TableRow{
			Customer: customers[i%len(customers)],
			Package:  packages[r.Intn(len(packages))],
			Panel:    panels[r.Intn(len(panels))],
			Status:   statuses[r.Intn(len(statuses))],
		})
	}
	return rows
}
