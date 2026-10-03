package portal

// Profile-scraping: once the browser lands on /subscribers/profile/<id>,
// we copy the page's visible text (the Playwright equivalent of
// Ctrl+A → Ctrl-C) and parse that raw text into the JSON schema below.
// The parser is pure Go, so it is unit-testable without a browser.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pw "github.com/mxschmitt/playwright-go"
)

// ErrNotLoggedIn means the portal redirected the profile request to the
// login page — the operator's session expired.
var ErrNotLoggedIn = errors.New("not logged in — session expired (redirected to /login)")

// Profile is the JSON schema produced from the raw Ctrl+A page text.
type Profile struct {
	// Header block
	Name        string `json:"name"`
	Username    string `json:"username"`
	ProfileType string `json:"profile_type"`
	Salesperson string `json:"salesperson"`
	Status      string `json:"status"` // Online/Offline

	// Usage cards (top row)
	OnlineUptime  string `json:"online_uptime"`
	TotalData     string `json:"total_data"`
	TotalDataUnit string `json:"total_data_unit"`
	UsedData      string `json:"used_data"`
	UsedDataUnit  string `json:"used_data_unit"`
	RemainingData string `json:"remaining_data"`
	RemainingUnit string `json:"remaining_data_unit"`
	Balance       string `json:"balance"`
	Due           string `json:"due"`
	Tickets       string `json:"tickets"`

	// Sections
	Personal   map[string]string `json:"personal_information"`
	Company    map[string]string `json:"company_information"`
	Connection map[string]string `json:"connection_information"`
	Package    map[string]string `json:"package_information"`
	Settings   map[string]string `json:"service_settings"`
	Service    map[string]string `json:"service_information"`
	Online     map[string]string `json:"online_details,omitempty"` // online-only session card
	Discount   map[string]string `json:"discount_and_quota"`
	Address    map[string]string `json:"address_information"`

	// Footer
	FooterRaw string `json:"footer_raw,omitempty"`

	// UnparsedRaw keeps the raw lines of any section whose label
	// vocabulary matched nothing — a debug breadcrumb for when PACE adds
	// a card the parser doesn't know yet.
	UnparsedRaw map[string][]string `json:"unparsed_raw,omitempty"`
}

// sectionHeaders must match the Ctrl+A text EXACTLY after trim.
// "Online Details" / "Session Statistics" only exist when the subscriber
// is online — without them the whole online session card would
// contaminate Service Information (the off-by-one garbage bug).
var sectionHeaders = map[string]string{
	"Personal Information":           "personal",
	"Company Information":            "company",
	"Connection Information":         "connection",
	"Package Information":            "package",
	"Service Settings":               "settings",
	"Service Information":            "service",
	"Online Details":                 "online",
	"Session Statistics":             "online",
	"Discount and Quota Information": "discount",
	"Additional Information":         "additional",
	"Address Information":            "address",
}

// knownLabels is the per-section label vocabulary: the anchor-based
// extractor only turns a line into a key when it appears here, so values
// can never become keys and foreign sections can never leak keys in.
var knownLabels = map[string]map[string]bool{
	"personal": {
		"Identity": true, "Phone": true, "Email": true,
	},
	"company": {
		"ISP": true, "Branch": true, "Salesperson": true, "Created At": true,
	},
	"connection": {
		"Profile Status": true, "Net Status": true, "Connection Type": true,
		"Connection Status": true, "NAS": true, "NAS Name": true,
	},
	"package": {
		"Package": true, "Package Duration": true, "Policy": true,
		"Connected Policy": true, "Total Policies": true, "Network Profile": true,
	},
	"settings": {
		"SMS Status": true, "Email Status": true, "Auto Mac Lock": true,
		"Auto Renew Status": true, "Lock Volume": true, "Lock Session": true,
	},
	"service": {
		"Expiration Date": true, "Last Expiration Date": true,
		"Last Activation Date": true, "Last Activation By": true,
	},
	"online": {
		"Leased IP Address": true, "Leased Ipv6 Address": true,
		"MAC Address": true, "Framed Protocol": true,
		"NAS Name": true, "NAS Port": true, "NAS Port Type": true,
		"Network Profile": true, "Connection Status": true,
		"Online Uptime": true, "Upload": true, "Download": true,
		"Session Started": true, "Session Updated": true,
		"Session Interval Time": true, "Technical Details": true,
		"Session Statistics": true,
	},
	"discount": {
		"Total Volume": true, "Used Volume": true, "Remaining Volume": true,
		"Discount (Flat-Rate)": true,
	},
	"address": {
		"Address": true,
	},
}

// usageCardLabels: card labels at the TOP (before Personal Information).
var usageCardLabels = map[string]string{
	"Online Uptime": "OnlineUptime",
	"Total":         "TotalData",
	"Used":          "UsedData",
	"Remaining":     "RemainingData",
	"Balance":       "Balance",
	"Due":           "Due",
	"Tickets":       "Tickets",
}

// Known units attached to the previous card.
var units = map[string]bool{"GB": true, "MB": true, "TB": true}

// scrapeProfile copies the profile page's visible text (Ctrl+A → Ctrl-C
// equivalent) and parses it into a Profile. Returns the profile plus the
// list of required fields that came back empty (for the PARTIAL state).
func scrapeProfile(page pw.Page) (*Profile, []string, error) {
	if strings.Contains(page.URL(), "/login") {
		return nil, nil, ErrNotLoggedIn
	}
	text, err := page.Locator("body").InnerText()
	if err != nil {
		return nil, nil, fmt.Errorf("copy page text: %s", cleanErr(err))
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil, errors.New("profile page text is empty")
	}

	prof := ParseRawText(text)
	missing := missingRequired(prof)
	return prof, missing, nil
}

// RequiredInfo is EXACTLY the information the operator needs, in the
// order it was specified: name, username/id, identity, phone, package,
// last activation date, last activation by, expiration date, status.
// It is written to the top of every customer file so the key fields are
// readable at a glance without digging through the section maps.
type RequiredInfo struct {
	Name               string `json:"name"`
	Username           string `json:"username"`
	Identity           string `json:"identity"`
	Phone              string `json:"phone"`
	Package            string `json:"pkg"`
	LastActivationDate string `json:"last_activation_date"`
	LastActivationBy   string `json:"last_activation_by"`
	ExpirationDate     string `json:"expiration_date"`
	Status             string `json:"status"` // Online / Offline
}

// RequiredInfoFrom flattens the profile into the operator's key fields.
func RequiredInfoFrom(p *Profile) RequiredInfo {
	if p == nil {
		return RequiredInfo{}
	}
	return RequiredInfo{
		Name:               p.Name,
		Username:           p.Username,
		Identity:           p.Personal["Identity"],
		Phone:              p.Personal["Phone"],
		Package:            p.Package["Package"],
		LastActivationDate: p.Service["Last Activation Date"],
		LastActivationBy:   p.Service["Last Activation By"],
		ExpirationDate:     p.Service["Expiration Date"],
		Status:             p.Status,
	}
}

// missingRequired lists which of the required fields came back empty
// (struct changed, row absent, never activated, …) — drives PARTIAL.
func missingRequired(p *Profile) []string {
	info := RequiredInfoFrom(p)
	fields := []struct {
		key   string
		value string
	}{
		{"name", info.Name},
		{"username", info.Username},
		{"identity", info.Identity},
		{"phone", info.Phone},
		{"pkg", info.Package},
		{"last activation date", info.LastActivationDate},
		{"last activation by", info.LastActivationBy},
		{"expiration date", info.ExpirationDate},
		{"status", info.Status},
	}
	var missing []string
	for _, f := range fields {
		if f.value == "" {
			missing = append(missing, f.key)
		}
	}
	return missing
}

// customerDoc is what lands in data/customers/<username>.json: the
// operator's key fields up top, the full parsed profile below, plus
// where/when it was scraped.
type customerDoc struct {
	QueryUsername string       `json:"query_username"` // username we searched for
	ProfileURL    string       `json:"profile_url"`
	ScrapedAt     time.Time    `json:"scraped_at"`
	Info          RequiredInfo `json:"info"`
	Profile       *Profile     `json:"profile"`
}

// saveCustomer writes data/customers/<username>.json atomically
// (temp + rename), reflecting the latest scrape. Best-effort: a failed
// write never fails the activation flow.
func (r *Runner) saveCustomer(username, profileURL string, prof *Profile) {
	if r.customersDir == "" || prof == nil {
		return
	}
	doc := customerDoc{
		QueryUsername: username,
		ProfileURL:    profileURL,
		ScrapedAt:     time.Now(),
		Info:          RequiredInfoFrom(prof),
		Profile:       prof,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(r.customersDir, 0o755); err != nil {
		return
	}
	path := filepath.Join(r.customersDir, username+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

// ------------------------------------------------------------
// Parser — works on the raw selected/copied page text
// ------------------------------------------------------------

// ParseRawText converts the Ctrl+A/Ctrl-C text of a subscriber profile
// page into the Profile JSON schema.
func ParseRawText(raw string) *Profile {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	allLines := strings.Split(raw, "\n")

	lines := make([]string, 0, len(allLines))
	for _, l := range allLines {
		l = strings.TrimSpace(l)
		if l == "" || l == "img" || l == "logo" {
			continue
		}
		lines = append(lines, l)
	}

	p := &Profile{
		Personal:   map[string]string{},
		Company:    map[string]string{},
		Connection: map[string]string{},
		Package:    map[string]string{},
		Settings:   map[string]string{},
		Service:    map[string]string{},
		Discount:   map[string]string{},
		Address:    map[string]string{},
	}

	// Phase 1: find "Personal Information" (end of header block).
	startIdx := -1
	for i, l := range lines {
		if l == "Personal Information" {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return p // nothing to parse
	}

	// Phase 2: header block.
	parseHeaderBlock(lines[:startIdx], p)

	// Phase 3: walk sections.
	i := startIdx
	for i < len(lines) {
		line := lines[i]
		sectionKey, isHeader := sectionHeaders[line]
		if !isHeader {
			i++
			continue
		}

		end := i + 1
		for end < len(lines) {
			if _, ok := sectionHeaders[lines[end]]; ok {
				break
			}
			if strings.HasPrefix(lines[end], "Created By ") {
				break
			}
			end++
		}

		body := lines[i+1 : end]
		vocab, hasVocab := knownLabels[sectionKey]

		// Anchor-based extraction: only vocabulary labels can become
		// keys, so empty values / foreign cards can't shift the pairing.
		var pairs map[string]string
		if hasVocab {
			pairs = extractPairsAnchored(body, vocab)
			// Debug breadcrumb: vocabulary matched nothing in a card
			// that had content — likely a new/renamed PACE card.
			if len(pairs) == 0 && len(body) > 0 {
				if p.UnparsedRaw == nil {
					p.UnparsedRaw = map[string][]string{}
				}
				p.UnparsedRaw[sectionKey] = body
			}
		}

		switch sectionKey {
		case "personal":
			p.Personal = pairs
		case "company":
			p.Company = pairs
		case "connection":
			p.Connection = pairs
		case "package":
			p.Package = pairs
		case "settings":
			p.Settings = pairs
		case "service":
			p.Service = pairs
		case "online":
			// Two headers ("Online Details", "Session Statistics") can
			// split the online card — merge instead of overwrite.
			if p.Online == nil {
				p.Online = map[string]string{}
			}
			for k, v := range pairs {
				p.Online[k] = v
			}
		case "discount":
			p.Discount = pairs
		case "address":
			p.Address = pairs
		}

		i = end
	}

	// Phase 4: footer.
	for _, l := range lines {
		if strings.HasPrefix(l, "Created By ") {
			p.FooterRaw = l
			break
		}
	}

	return p
}

// parseHeaderBlock handles name/username, badges, and usage cards.
func parseHeaderBlock(lines []string, p *Profile) {
	// 1. Name + username: "Muhammad Sabir (hp_m.sabir_anjeera)"
	for _, l := range lines {
		if strings.Contains(l, "(") && strings.HasSuffix(l, ")") &&
			strings.Contains(l, "_") {
			parts := strings.SplitN(l, "(", 2)
			p.Name = strings.TrimSpace(parts[0])
			p.Username = strings.TrimSuffix(strings.TrimSpace(parts[1]), ")")
			break
		}
	}

	// 2. Badges: "Subscriber", "admin (Admin)", "Offline" — right after
	//    the name line.
	nameIdx := -1
	for i, l := range lines {
		if p.Username != "" && strings.Contains(l, p.Username) {
			nameIdx = i
			break
		}
	}
	if nameIdx >= 0 {
		for i := nameIdx + 1; i < len(lines) && i < nameIdx+5; i++ {
			l := lines[i]
			switch {
			case l == "Subscriber":
				p.ProfileType = l
			case strings.HasPrefix(l, "admin") || strings.HasPrefix(l, "Admin"):
				p.Salesperson = l
			case l == "Online" || l == "Offline":
				if p.Status == "" {
					p.Status = l
				}
			}
		}
	}

	// 3. Usage cards: LABEL \n VALUE [\n UNIT]
	for i := 0; i < len(lines); i++ {
		label := lines[i]
		target, ok := usageCardLabels[label]
		if !ok {
			continue
		}
		if i+1 >= len(lines) {
			continue
		}
		value := lines[i+1]
		unit := ""
		if i+2 < len(lines) && units[lines[i+2]] {
			unit = lines[i+2]
		}

		switch target {
		case "OnlineUptime":
			p.OnlineUptime = value
		case "TotalData":
			p.TotalData = value
			p.TotalDataUnit = unit
		case "UsedData":
			p.UsedData = value
			p.UsedDataUnit = unit
		case "RemainingData":
			p.RemainingData = value
			p.RemainingUnit = unit
		case "Balance":
			p.Balance = value
		case "Due":
			p.Due = value
		case "Tickets":
			p.Tickets = value
		}
	}
}

// extractPairsAnchored parses a section body using a label whitelist
// (the anchors). Robust to:
//   - reversed order (values appearing near labels differently)
//   - empty values (label followed immediately by another label)
//   - multi-line values (first non-label line wins, unit lines attached)
//   - unknown extra lines (never become keys — only vocabulary labels do)
func extractPairsAnchored(body []string, known map[string]bool) map[string]string {
	out := map[string]string{}

	type anchor struct {
		idx   int
		label string
	}
	var anchors []anchor
	for i, line := range body {
		// Some PACE cards render labels with a trailing colon.
		clean := strings.TrimSuffix(strings.TrimSpace(line), ":")
		if known[clean] {
			anchors = append(anchors, anchor{idx: i, label: clean})
		}
	}
	if len(anchors) == 0 {
		return out
	}

	// Each anchor owns the region between itself and the next anchor.
	for i, a := range anchors {
		next := len(body)
		if i+1 < len(anchors) {
			next = anchors[i+1].idx
		}
		region := body[a.idx+1 : next]

		value := ""
		for j, c := range region {
			c = strings.TrimSpace(c)
			// Skip empties, footer noise, and label-looking lines from
			// any section (a foreign label means "no value here").
			if c == "" || junkLine(c) || isKnownLabel(c) {
				continue
			}
			value = c
			if j+1 < len(region) {
				if u := strings.TrimSpace(region[j+1]); units[u] {
					value += " " + u
				}
			}
			break
		}
		out[a.label] = value
	}

	return out
}

// junkLine reports footer/meta noise that should never be a value.
func junkLine(s string) bool {
	return strings.HasPrefix(s, "Created By ") ||
		strings.HasPrefix(s, "v2.") ||
		s == ","
}

// isKnownLabel reports whether s is a label in ANY section vocabulary.
// Built from knownLabels so there is a single source of truth.
func isKnownLabel(s string) bool {
	for _, vocab := range knownLabels {
		if vocab[s] {
			return true
		}
	}
	return false
}
