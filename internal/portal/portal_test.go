package portal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractUsernames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"please activate hp_wasif", []string{"hp_wasif"}},
		{"mm_wasif", []string{"mm_wasif"}},
		{"hp_shahzad_ahmad_Mkund done", []string{"hp_shahzad_ahmad_Mkund"}},
		{"bill for sk_ali and kts_xyz", []string{"sk_ali", "kts_xyz"}},
		{"activate mm_wasif and MM_Wasif", []string{"mm_wasif"}}, // case-insensitive dedupe
		{"duplicate hp_x hp_x hp_X", []string{"hp_x"}},
		{"no username here at all", nil},
		{"12345 nope_ _alone", nil},         // needs a char after the underscore
		{"mm_wasif,", []string{"mm_wasif"}}, // punctuation boundary
		{"", nil},
	}
	for _, c := range cases {
		got := ExtractUsernames(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ExtractUsernames(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ExtractUsernames(%q) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestOrigin(t *testing.T) {
	cases := map[string]string{
		"http://103.67.54.54/":           "http://103.67.54.54",
		"https://portal.example.com/x/y": "https://portal.example.com",
		"not a url":                      "not a url",
	}
	for in, want := range cases {
		if got := origin(in); got != want {
			t.Errorf("origin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestActivationLogAppendsAndReloads(t *testing.T) {
	dir := t.TempDir()
	r := NewRunner(nil, "http://103.67.54.54/", dir)

	r.appendRecord(ResultEvent{Username: "hp_a", Chat: "1@g.us", At: time.Now()})
	r.appendRecord(ResultEvent{
		Username:   "hp_b",
		Chat:       "1@g.us",
		ProfileURL: "http://103.67.54.54/subscribers/profile/9",
		At:         time.Now(),
	})
	r.appendRecord(ResultEvent{Username: "hp_c", Err: errFake{}, At: time.Now()})

	if r.Count() != 3 {
		t.Fatalf("Count = %d, want 3", r.Count())
	}

	// Fresh runner must see the full history.
	r2 := NewRunner(nil, "http://103.67.54.54/", dir)
	if r2.Count() != 3 {
		t.Fatalf("reloaded Count = %d, want 3", r2.Count())
	}
	if r2.records[1].ProfileURL == "" || r2.records[2].Error == "" {
		t.Errorf("history lost fields: %+v", r2.records)
	}

	// File must exist and be valid JSON.
	if _, err := os.Stat(filepath.Join(dir, "activation_log.json")); err != nil {
		t.Errorf("log file missing: %v", err)
	}
}

type errFake struct{}

func (errFake) Error() string { return "boom" }

func TestEnqueueGuards(t *testing.T) {
	// nil runner never panics, always refuses.
	var nilRunner *Runner
	if nilRunner.Enqueue(Job{Username: "hp_a"}) {
		t.Error("nil runner accepted a job")
	}
	if nilRunner.Events() != nil {
		t.Error("nil runner events should be nil")
	}

	// Dedupe: second enqueue of a queued username is refused. (The first
	// job can't be processed here — there is no real page — so it stays
	// in the queue, which is exactly what we're testing against.)
	r := NewRunner(nil, "http://103.67.54.54/", t.TempDir())
	if !r.Enqueue(Job{Username: "hp_a"}) {
		t.Fatal("first enqueue refused")
	}
	if r.Enqueue(Job{Username: "hp_a"}) {
		t.Error("duplicate enqueue accepted")
	}
	if !r.Enqueue(Job{Username: "hp_b"}) {
		t.Error("different username refused")
	}
	if r.Enqueue(Job{Username: ""}) {
		t.Error("empty username accepted")
	}
}
