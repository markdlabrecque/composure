package store

import (
	"context"
	"reflect"
	"testing"
)

func TestMediaSchemaInitialized(t *testing.T) {
	s, _ := accountSite(t)
	rows, err := s.db.Query("PRAGMA table_info(media)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var position, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&position, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "storage_name", "kind", "size", "width", "height", "created_by"}
	if !reflect.DeepEqual(columns, want) {
		t.Fatalf("media columns = %v, want %v", columns, want)
	}
}

func TestOpenDoesNotInstallMissingMediaSchema(t *testing.T) {
	s, path := accountSite(t)
	if _, err := s.db.Exec("DROP TABLE media"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var tables int
	if err := reopened.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='media'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("ordinary startup installed missing media schema")
	}
}
