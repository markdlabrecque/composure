package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSignInLogicalSnapshotHelper(t *testing.T) {
	path := os.Getenv("COMPOSURE_TEST_SIGNIN_SNAPSHOT_DB")
	if path == "" {
		t.Skip("test-only sign-in snapshot subprocess")
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	rows, err := s.db.Query(`SELECT name FROM sqlite_schema
		WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name <> 'throttle_counters'
		ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	hash := sha256.New()
	for _, table := range tables {
		fmt.Fprintf(hash, "table:%s\n", table)
		query := `SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY rowid`
		tableRows, err := s.db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := tableRows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(hash, "columns:%q\n", columns)
		for tableRows.Next() {
			values := make([]any, len(columns))
			destinations := make([]any, len(columns))
			for i := range values {
				destinations[i] = &values[i]
			}
			if err := tableRows.Scan(destinations...); err != nil {
				t.Fatal(err)
			}
			for _, value := range values {
				switch value := value.(type) {
				case []byte:
					fmt.Fprintf(hash, "blob:%s\n", hex.EncodeToString(value))
				default:
					fmt.Fprintf(hash, "%T:%v\n", value, value)
				}
			}
		}
		if err := tableRows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("SIGNIN_LOGICAL_SNAPSHOT=%x", hash.Sum(nil))
}
