package db

import (
	"strings"
	"testing"
)

func TestSplitSQLStatementsIgnoresSemicolonsInStringsAndComments(t *testing.T) {
	source := "CREATE TABLE t(v TEXT); INSERT INTO t VALUES('a;b'); -- ignore ; here\nUPDATE t SET v=\"x;y\"; /* ; */ DELETE FROM t;"
	got := splitSQLStatements(source)
	want := []string{"CREATE TABLE t(v TEXT)", " INSERT INTO t VALUES('a;b')", " -- ignore ; here\nUPDATE t SET v=\"x;y\"", " /* ; */ DELETE FROM t"}
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != strings.TrimSpace(want[i]) {
			t.Errorf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
}
