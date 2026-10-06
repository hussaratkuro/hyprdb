package db

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	script := "-- c\nSELECT 'a;b', \"x;\" ; /* ; */ INSERT INTO t VALUES ('it''s;');\n" +
		"DELIMITER //\nCREATE PROCEDURE p() BEGIN SELECT 1; END//\nDELIMITER ;\n" +
		"DO $$ BEGIN RAISE NOTICE 'x;'; END $$;\n#tail\n"
	got := SplitStatements(script)
	want := []string{"SELECT 'a;b', \"x;\"", "INSERT INTO t VALUES ('it''s;')", "CREATE PROCEDURE p() BEGIN SELECT 1; END", "DO $$ BEGIN RAISE NOTICE 'x;'; END $$"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestLiteral(t *testing.T) {
	my, pg := &Conn{Cfg: Config{Kind: MariaDB}}, &Conn{Cfg: Config{Kind: Postgres}}
	if s := my.literal(Value{Text: `a'b\c`}, "VARCHAR"); s != `'a\'b\\c'` {
		t.Fatal(s)
	}
	if s := pg.literal(Value{Text: `a'b`}, "TEXT"); s != `'a''b'` {
		t.Fatal(s)
	}
	if s := pg.literal(Value{Text: "\x01"}, "BYTEA"); s != `'\x01'` {
		t.Fatal(s)
	}
	if s := my.literal(Value{Null: true}, "INT"); s != "NULL" {
		t.Fatal(s)
	}
}

func TestDefaultExpr(t *testing.T) {
	my := &Conn{Cfg: Config{Kind: MySQL}}
	for in, want := range map[string]string{"0": "0", "1.5": "1.5", "NULL": "NULL", "CURRENT_TIMESTAMP": "CURRENT_TIMESTAMP", "now()": "now()", "abc": "'abc'", "": "''"} {
		if got := my.defaultExpr(in); got != want {
			t.Errorf("defaultExpr(%q) = %q, want %q", in, got, want)
		}
	}
}
