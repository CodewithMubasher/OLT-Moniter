package portal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleProfileText mimics the Ctrl+A → Ctrl-C copy of a real profile
// page (header block → sections → footer).
const sampleProfileText = `Nadir Amin Khan (hp_nadir_derwesh)
Subscriber
admin (Staff)
Offline
Online Uptime
05:12:33
Total
100
GB
Used
25
GB
Remaining
75
GB
Balance
0.00
Due
0.00
Personal Information
Identity
123456
Phone
923345003636
Email
nadir@example.com
Company Information
ISP
MyISP
Branch
Karachi
Created At
01 Jan 2026
Connection Information
Profile Status
Active
Net Status
Enabled
Connection Type
PPPoE
Connection Status
Offline
NAS
nas-1
Package Information
Package
Package15Mbps
Package Duration
1 Month
Policy
pppoe15mb
Service Settings
SMS Status
Enabled
Service Information
Expiration Date
16 Oct 2026 11:59:00
Last Activation Date
03 Oct 2026, 07:27 PM
Last Activation By
humaira (Staff)
Discount and Quota Information
Total Volume
50
GB
Address Information
Address
House 1, Street 2
Created By humaira (Staff)
v2.1.0
`

func TestParseRawText(t *testing.T) {
	p := ParseRawText(sampleProfileText)

	if p.Name != "Nadir Amin Khan" {
		t.Errorf("Name = %q, want %q", p.Name, "Nadir Amin Khan")
	}
	if p.Username != "hp_nadir_derwesh" {
		t.Errorf("Username = %q, want %q", p.Username, "hp_nadir_derwesh")
	}
	if p.ProfileType != "Subscriber" {
		t.Errorf("ProfileType = %q, want Subscriber", p.ProfileType)
	}
	if p.Status != "Offline" {
		t.Errorf("Status = %q, want Offline", p.Status)
	}
	if p.OnlineUptime != "05:12:33" {
		t.Errorf("OnlineUptime = %q, want 05:12:33", p.OnlineUptime)
	}
	if p.TotalData != "100" || p.TotalDataUnit != "GB" {
		t.Errorf("TotalData = %q %q, want 100 GB", p.TotalData, p.TotalDataUnit)
	}
	if p.UsedData != "25" || p.UsedDataUnit != "GB" {
		t.Errorf("UsedData = %q %q, want 25 GB", p.UsedData, p.UsedDataUnit)
	}

	if got := p.Personal["Phone"]; got != "923345003636" {
		t.Errorf("Personal[Phone] = %q, want 923345003636", got)
	}
	if got := p.Personal["Identity"]; got != "123456" {
		t.Errorf("Personal[Identity] = %q, want 123456", got)
	}
	// Exact "Package" label — must not be clobbered by "Package Duration".
	if got := p.Package["Package"]; got != "Package15Mbps" {
		t.Errorf("Package[Package] = %q, want Package15Mbps", got)
	}
	if got := p.Package["Package Duration"]; got != "1 Month" {
		t.Errorf("Package[Package Duration] = %q, want 1 Month", got)
	}
	if got := p.Package["Policy"]; got != "pppoe15mb" {
		t.Errorf("Package[Policy] = %q, want pppoe15mb", got)
	}
	if got := p.Service["Expiration Date"]; got != "16 Oct 2026 11:59:00" {
		t.Errorf("Service[Expiration Date] = %q", got)
	}
	if got := p.Service["Last Activation Date"]; got != "03 Oct 2026, 07:27 PM" {
		t.Errorf("Service[Last Activation Date] = %q, want 03 Oct 2026, 07:27 PM", got)
	}
	if got := p.Service["Last Activation By"]; got != "humaira (Staff)" {
		t.Errorf("Service[Last Activation By] = %q, want humaira (Staff)", got)
	}
	// Absent rows simply stay empty — no error.
	if got := p.Service["Last Expiration Date"]; got != "" {
		t.Errorf("Service[Last Expiration Date] = %q, want empty", got)
	}
	if got := p.Connection["Connection Status"]; got != "Offline" {
		t.Errorf("Connection[Connection Status] = %q, want Offline", got)
	}
	if p.FooterRaw != "Created By humaira (Staff)" {
		t.Errorf("FooterRaw = %q", p.FooterRaw)
	}
}

// sampleProfileTextOnline mimics an ONLINE subscriber: PACE injects an
// "Online Details" card (plus a "Session Statistics" sub-card) between
// Service Information and Discount — the exact layout that used to
// contaminate service_information with off-by-one garbage.
const sampleProfileTextOnline = `Zulfiqar Ahmad (hp_zulfiqar_ahmad_rajabad)
Subscriber
admin (Staff)
Online
Personal Information
Identity
1330205215195
Phone
923068928576
Package Information
Package
6mbps_policy
Package Duration
1 Month
Policy
6mbps
Service Information
Expiration Date
05 Nov 2026 23:59:00
Last Expiration Date
05 Nov 2026 11:59:00
Last Activation Date
03 Oct 2026, 04:59 PM
Last Activation By
humaira (Staff)
Online Details
Connection Status
Online
Online Uptime
12h 51m
Leased IP Address
100.64.4.21
Leased Ipv6 Address
MAC Address
AC:F9:70:98:71:2F
Framed Protocol
PPP
NAS Name
R730_Server
NAS Port
vlan114
NAS Port Type
Ethernet
Network Profile
service11
Session Statistics
Session Started
2026-10-03 07:14:41
Technical Details
3.00Min
Upload
4,316.49 MB
Download
617.94 MB
Session Updated
2026-10-03 07:14:41
Session Interval Time
2026-10-03 20:05:43
Discount and Quota Information
Total Volume
50
GB
Created By humaira (Staff)
`

// TestParseRawTextOnlineNoContamination: the online session card must
// land in online_details, never bleed into service_information, and no
// value may ever become a key.
func TestParseRawTextOnlineNoContamination(t *testing.T) {
	p := ParseRawText(sampleProfileTextOnline)

	// service_information holds EXACTLY the date rows — no session junk.
	wantService := map[string]string{
		"Expiration Date":      "05 Nov 2026 23:59:00",
		"Last Expiration Date": "05 Nov 2026 11:59:00",
		"Last Activation Date": "03 Oct 2026, 04:59 PM",
		"Last Activation By":   "humaira (Staff)",
	}
	if len(p.Service) != len(wantService) {
		t.Errorf("service_information = %+v, want exactly %d date rows", p.Service, len(wantService))
	}
	for k, want := range wantService {
		if got := p.Service[k]; got != want {
			t.Errorf("service_information[%q] = %q, want %q", k, got, want)
		}
	}
	// The old bug: values as keys ("12h 51m", "100.64.4.21", …).
	for _, bad := range []string{"12h 51m", "100.64.4.21", "Online Uptime", "Upload"} {
		if _, ok := p.Service[bad]; ok {
			t.Errorf("service_information leaked key %q (off-by-one bug is back)", bad)
		}
	}

	// online_details carries the session card cleanly.
	if p.Online == nil {
		t.Fatal("online_details not captured")
	}
	onlineWant := map[string]string{
		"Connection Status":     "Online",
		"Online Uptime":         "12h 51m",
		"Leased IP Address":     "100.64.4.21",
		"Leased Ipv6 Address":   "", // empty value must stay empty, not shift
		"MAC Address":           "AC:F9:70:98:71:2F",
		"NAS Name":              "R730_Server",
		"Upload":                "4,316.49 MB",
		"Download":              "617.94 MB",
		"Session Started":       "2026-10-03 07:14:41",
		"Technical Details":     "3.00Min",
		"Session Interval Time": "2026-10-03 20:05:43",
	}
	for k, want := range onlineWant {
		if got := p.Online[k]; got != want {
			t.Errorf("online_details[%q] = %q, want %q", k, got, want)
		}
	}

	// The operator's key fields still parse for the online subscriber.
	info := RequiredInfoFrom(p)
	if info.Identity != "1330205215195" || info.Phone != "923068928576" ||
		info.Package != "6mbps_policy" || info.Status != "Online" {
		t.Errorf("required info wrong for online subscriber: %+v", info)
	}
	if missing := missingRequired(p); len(missing) != 0 {
		t.Errorf("online profile missing = %v, want none", missing)
	}
}

func TestMissingRequired(t *testing.T) {
	full := ParseRawText(sampleProfileText)
	if missing := missingRequired(full); len(missing) != 0 {
		t.Errorf("complete profile missing = %v, want none", missing)
	}

	empty := &Profile{}
	missing := missingRequired(empty)
	if len(missing) != 9 {
		t.Fatalf("empty profile missing = %v, want 9 entries", missing)
	}
	want := strings.Join(missing, ",")
	for _, w := range []string{
		"name", "username", "identity", "phone", "pkg",
		"last activation date", "last activation by",
		"expiration date", "status",
	} {
		if !strings.Contains(want, w) {
			t.Errorf("missing list %v lacks %q", missing, w)
		}
	}

	// Login-page text: nothing parses → everything required is missing.
	garbage := ParseRawText("Login\nUsername\nPassword")
	if missing := missingRequired(garbage); len(missing) != 9 {
		t.Errorf("login text missing = %v, want 9 entries", missing)
	}
}

// TestRequiredInfo: the operator's exact field list is flattened at the
// top of every customer file.
func TestRequiredInfo(t *testing.T) {
	info := RequiredInfoFrom(ParseRawText(sampleProfileText))
	if info.Name != "Nadir Amin Khan" || info.Username != "hp_nadir_derwesh" {
		t.Errorf("name/username = %q / %q", info.Name, info.Username)
	}
	if info.Identity != "123456" || info.Phone != "923345003636" {
		t.Errorf("identity/phone = %q / %q", info.Identity, info.Phone)
	}
	if info.Package != "Package15Mbps" {
		t.Errorf("pkg = %q", info.Package)
	}
	if info.LastActivationDate != "03 Oct 2026, 07:27 PM" {
		t.Errorf("last activation date = %q", info.LastActivationDate)
	}
	if info.LastActivationBy != "humaira (Staff)" {
		t.Errorf("last activation by = %q", info.LastActivationBy)
	}
	if info.ExpirationDate != "16 Oct 2026 11:59:00" {
		t.Errorf("expiration date = %q", info.ExpirationDate)
	}
	if info.Status != "Offline" {
		t.Errorf("status = %q, want Offline", info.Status)
	}
}

func TestProfileMarshalsToValidJSON(t *testing.T) {
	p := ParseRawText(sampleProfileText)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round Profile
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Username != p.Username || round.Package["Package"] != p.Package["Package"] {
		t.Errorf("JSON round-trip lost data: %+v", round)
	}
}

// TestSaveCustomer writes the per-customer file and reads it back.
func TestSaveCustomer(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(nil, "http://127.0.0.1/", dir)
	p := ParseRawText(sampleProfileText)

	r.saveCustomer("hp_nadir_derwesh", "http://103.67.54.54/subscribers/profile/3387", p)

	path := filepath.Join(dir, "customers", "hp_nadir_derwesh.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("customer file: %v", err)
	}
	var doc struct {
		QueryUsername string   `json:"query_username"`
		Profile       *Profile `json:"profile"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("customer file json: %v", err)
	}
	if doc.QueryUsername != "hp_nadir_derwesh" {
		t.Errorf("query_username = %q", doc.QueryUsername)
	}
	if doc.Profile == nil || doc.Profile.Name != "Nadir Amin Khan" {
		t.Errorf("profile not saved correctly: %+v", doc.Profile)
	}
}

// TestActivationRecordKeepsProfile: the profile and missing fields ride
// along in activation_log.json.
func TestActivationRecordKeepsProfile(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(nil, "http://127.0.0.1/", dir)

	res := ResultEvent{
		Username:   "hp_nadir_derwesh",
		Chat:       "1@g.us",
		ProfileURL: "http://103.67.54.54/subscribers/profile/3387",
		Profile:    ParseRawText(sampleProfileText),
		Missing:    []string{"phone"},
		At:         time.Now(),
	}
	r.appendRecord(res)

	recs := loadRecords(r.logPath)
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	rec := recs[0]
	if rec.Profile == nil || rec.Profile.Personal["Phone"] != "923345003636" {
		t.Errorf("record profile not persisted: %+v", rec.Profile)
	}
	if len(rec.MissingFields) != 1 || rec.MissingFields[0] != "phone" {
		t.Errorf("missing_fields = %v, want [phone]", rec.MissingFields)
	}
}

func TestResultStringPartial(t *testing.T) {
	got := ResultEvent{Username: "hp_x", Missing: []string{"phone", "package"}}.String()
	if !strings.HasPrefix(got, "◐ hp_x (missing: phone, package)") {
		t.Errorf("String() = %q, want partial line", got)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("String() leaked a newline: %q", got)
	}
}
