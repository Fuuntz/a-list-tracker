package api

import (
	"archive/zip"
	"bytes"
	"testing"
)

const sampleDiary = `Date,Name,Year,Letterboxd URI,Rating,Rewatch,Tags,Watched Date
2026-08-02,"Paris, Texas",1984,https://boxd.it/2b3i,4.5,No,,2026-08-01
2026-08-04,Alien,1979,https://boxd.it/2awY,5,Yes,,2026-08-03
`

func TestParseLetterboxdCSV(t *testing.T) {
	rows, err := parseLetterboxdExport("diary.csv", []byte(sampleDiary))
	if err != nil {
		t.Fatalf("parseLetterboxdExport returned an error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].title != "Paris, Texas" || rows[0].watchedOn.Format("2006-01-02") != "2026-08-01" {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[0].canonicalKey == nil || *rows[0].canonicalKey == "" {
		t.Fatal("expected a canonical key")
	}
}

func TestParseLetterboxdZIP(t *testing.T) {
	var contents bytes.Buffer
	archive := zip.NewWriter(&contents)
	file, err := archive.Create("letterboxd-user-2026-08-08/diary.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(sampleDiary)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := parseLetterboxdExport("export.zip", contents.Bytes())
	if err != nil {
		t.Fatalf("parseLetterboxdExport returned an error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestParseLetterboxdCSVRequiresWatchedDate(t *testing.T) {
	_, err := parseLetterboxdExport("diary.csv", []byte("Name,Year\nAlien,1979\n"))
	if err == nil {
		t.Fatal("expected missing watched date to be rejected")
	}
}

func TestSafeSpreadsheetCell(t *testing.T) {
	if got := safeSpreadsheetCell("=HYPERLINK(\"bad\")"); got[0] != '\'' {
		t.Fatalf("expected formula-like value to be escaped, got %q", got)
	}
	if got := safeSpreadsheetCell("Normal title"); got != "Normal title" {
		t.Fatalf("expected normal title to remain unchanged, got %q", got)
	}
}
