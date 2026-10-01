package commonpasswords

import (
	"crypto/sha256"
	"fmt"
	"go/build"
	"os"
	"path"
	"strings"
	"sync"
	"testing"
)

const (
	corpusFile    = "10k-most-common.txt"
	corpusCommit  = "e749176aa4e3261ff41f7d197d7a01a0a705030e"
	corpusURL     = "https://raw.githubusercontent.com/danielmiessler/SecLists/" + corpusCommit + "/Passwords/Common-Credentials/" + corpusFile
	corpusSHA256  = "68782d6a4a19a4768d5f15dd66bd534e7a33055cc755411e33f16d18c50fdcce"
	licenseSHA256 = "3dbdc93d5f8829de0941744841730a09c106d0732e5ae0e98ca1d77be7ded66c"
)

// All password fixtures here are public corpus entries or invented test values.
// This package only answers membership; warning and rejection policy is outside it.
func TestContainsExactBytes(t *testing.T) {
	var lookup func(string) bool = Contains
	for _, tc := range []struct {
		name     string
		password string
		want     bool
	}{
		{"listed_password", "password", true},
		{"listed_numeric", "123456", true},
		{"listed_dragon", "dragon", true},
		{"unlisted", "a-long-unlisted-Composure-fixture-928461!", false},
		{"empty", "", false},
		{"uppercase", "DRAGON", false},
		{"mixed_case", "Password", false},
		{"leading_space", " password", false},
		{"trailing_space", "password ", false},
		{"surrounding_whitespace", "\tpassword\r\n", false},
		{"composed_unicode", "caf\u00e9", false},
		{"decomposed_unicode", "cafe\u0301", false},
		{"leading_nul", "\x00password", false},
		{"trailing_nul", "password\x00", false},
		{"nul_suffix", "password\x00ignored", false},
		{"invalid_utf8", "password\xff", false},
		{"long_suffix", "password" + strings.Repeat("x", 1024), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lookup(tc.password); got != tc.want {
				t.Errorf("Contains(%q) = %v, want %v", tc.password, got, tc.want)
			}
		})
	}
}

func TestContainsConcurrentMembership(t *testing.T) {
	fixtures := []struct {
		password string
		want     bool
	}{
		{"password", true}, {"123456", true}, {"dragon", true},
		{"DRAGON", false}, {"password ", false}, {"", false},
		{"a-long-unlisted-Composure-fixture-928461!", false},
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for iteration := 0; iteration < 100; iteration++ {
				for _, fixture := range fixtures {
					if got := Contains(fixture.password); got != fixture.want {
						t.Errorf("worker %d iteration %d: Contains(%q) = %v, want %v", worker, iteration, fixture.password, got, fixture.want)
						return
					}
				}
			}
		}(worker)
	}
	workers.Wait()
}

func TestContainsIndependentWorkingDirectory(t *testing.T) {
	// Keep this and the other TestContains tests free of source-file reads so
	// the reviewer can also run the compiled binary from an empty directory.
	t.Chdir(t.TempDir())
	if !Contains("password") || !Contains("dragon") || Contains("password ") || Contains("") {
		t.Fatal("membership changed when the working directory had no corpus asset")
	}
}

func TestPinnedCorpusAndCompleteMembership(t *testing.T) {
	data := readCommonPasswordFile(t, corpusFile)
	if got := len(data); got != 73026 {
		t.Fatalf("corpus size = %d bytes, want 73026", got)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != corpusSHA256 {
		t.Fatalf("corpus SHA-256 = %s, want pinned %s", got, corpusSHA256)
	}
	// Remove exactly the final line delimiter, never whitespace from entries.
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("pinned corpus must retain its final LF")
	}
	entries := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(entries) != 10001 {
		t.Fatalf("corpus entries = %d, want 10001; do not truncate to the filename's 10k", len(entries))
	}
	set := make(map[string]bool, len(entries))
	for i, entry := range entries {
		if entry == "" || set[entry] {
			t.Fatalf("empty or duplicate corpus entry at line %d", i+1)
		}
		set[entry] = true
	}
	for i, entry := range entries {
		if !Contains(entry) {
			t.Fatalf("Contains omitted pinned corpus entry at line %d", i+1)
		}
		// Case variants may themselves be listed. Compare exact bytes against
		// the complete pinned set rather than assume every variant is absent.
		for variant, candidate := range []string{
			strings.ToUpper(entry), strings.ToLower(entry),
			" " + entry, entry + " ", entry + "\x00", entry + "\x00suffix",
		} {
			if got, want := Contains(candidate), set[candidate]; got != want {
				t.Fatalf("line %d variant %d: Contains = %v, want exact membership %v", i+1, variant, got, want)
			}
		}
	}
}

func TestCorpusEmbeddedInProduction(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range pkg.EmbedPatterns {
		matched, err := path.Match(strings.TrimPrefix(pattern, "all:"), corpusFile)
		if err != nil {
			t.Fatal(err)
		}
		if matched {
			return
		}
	}
	t.Fatal("production Go source must embed the pinned corpus at build time, not read or fetch it at runtime")
}

func TestPinnedLicenseAndProvenance(t *testing.T) {
	license := readCommonPasswordFile(t, "LICENSE")
	if got := fmt.Sprintf("%x", sha256.Sum256(license)); got != licenseSHA256 {
		t.Fatalf("LICENSE SHA-256 = %s, want complete upstream MIT license %s", got, licenseSHA256)
	}
	source := string(readCommonPasswordFile(t, "SOURCE.md"))
	for _, required := range []string{"SecLists", corpusCommit, corpusURL, corpusSHA256, "MIT", "LICENSE"} {
		if !strings.Contains(source, required) {
			t.Errorf("SOURCE.md must record %q", required)
		}
	}
	if !strings.Contains(source, "10001") && !strings.Contains(source, "10,001") {
		t.Error("SOURCE.md must record the full corpus's 10001 entries")
	}
}

func readCommonPasswordFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read retained %s: %v", name, err)
	}
	return data
}
