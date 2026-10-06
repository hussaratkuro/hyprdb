package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Value is one cell; Null distinguishes SQL NULL from an empty string.
type Value struct {
	Text string
	Null bool
}

func (v Value) arg() any {
	if v.Null {
		return nil
	}
	return v.Text
}

type Result struct {
	Columns  []string
	Types    []string
	Rows     [][]Value
	Affected int64
	Elapsed  time.Duration
	IsQuery  bool
}

func (r *Result) Col(name string) int {
	for i, c := range r.Columns {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

func (r *Result) Get(row []Value, col string) string {
	if i := r.Col(col); i >= 0 && !row[i].Null {
		return row[i].Text
	}
	return ""
}

var queryWords = map[string]bool{"select": true, "show": true, "with": true, "explain": true, "describe": true, "desc": true, "values": true, "table": true, "analyze": true}

// Query runs one statement; result sets are read fully into memory.
func (c *Conn) Query(ctx context.Context, q string) (*Result, error) {
	start := time.Now()
	word := strings.ToLower(firstWord(q))
	if !queryWords[word] {
		res, err := c.DB.ExecContext(ctx, q)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		return &Result{Affected: n, Elapsed: time.Since(start)}, nil
	}
	rows, err := c.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r, err := readRows(rows)
	if err != nil {
		return nil, err
	}
	r.Elapsed = time.Since(start)
	return r, nil
}

func firstWord(q string) string {
	for _, l := range strings.Split(q, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "--") || strings.HasPrefix(l, "#") {
			continue
		}
		if i := strings.IndexAny(l, " \t("); i > 0 {
			return l[:i]
		}
		return l
	}
	return ""
}

func readRows(rows *sql.Rows) (*Result, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	r := &Result{Columns: cols, IsQuery: true}
	if cts, err := rows.ColumnTypes(); err == nil {
		for _, ct := range cts {
			r.Types = append(r.Types, ct.DatabaseTypeName())
		}
	}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]Value, len(cols))
		for i, v := range raw {
			row[i] = format(v)
		}
		r.Rows = append(r.Rows, row)
	}
	return r, rows.Err()
}

func format(v any) Value {
	switch x := v.(type) {
	case nil:
		return Value{Null: true}
	case []byte:
		return Value{Text: string(x)}
	case string:
		return Value{Text: x}
	case time.Time:
		if x.Hour()|x.Minute()|x.Second()|x.Nanosecond() == 0 {
			return Value{Text: x.Format("2006-01-02")}
		}
		return Value{Text: x.Format("2006-01-02 15:04:05.999999")}
	}
	return Value{Text: fmt.Sprint(v)}
}

// Page loads one window of a table ordered by its primary key.
func (c *Conn) Page(ctx context.Context, schema, table string, pk []string, where string, offset, limit int) (*Result, int64, error) {
	from := " FROM " + c.QT(schema, table)
	if where = strings.TrimSpace(where); where != "" {
		from += " WHERE " + where
	}
	var total int64
	if err := c.DB.QueryRowContext(ctx, "SELECT COUNT(*)"+from).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := "SELECT *" + from
	if len(pk) > 0 {
		qs := make([]string, len(pk))
		for i, k := range pk {
			qs[i] = c.Q(k)
		}
		q += " ORDER BY " + strings.Join(qs, ", ")
	}
	q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	res, err := c.Query(ctx, q)
	return res, total, err
}

func (c *Conn) whereArgs(res *Result, row []Value, pk []string, args []any) (string, []any) {
	parts := make([]string, len(pk))
	for i, k := range pk {
		v := row[res.Col(k)]
		if v.Null {
			parts[i] = c.Q(k) + " IS NULL"
			continue
		}
		parts[i] = c.Q(k) + " = " + c.ph(len(args))
		args = append(args, v.Text)
	}
	return strings.Join(parts, " AND "), args
}

func (c *Conn) UpdateCell(ctx context.Context, schema, table string, pk []string, res *Result, row []Value, col string, v Value) error {
	if len(pk) == 0 {
		return fmt.Errorf("%s has no primary key; edit it with a query", table)
	}
	where, args := c.whereArgs(res, row, pk, []any{v.arg()})
	r, err := c.DB.ExecContext(ctx, "UPDATE "+c.QT(schema, table)+" SET "+c.Q(col)+" = "+c.ph(0)+" WHERE "+where, args...)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 && c.IsMySQL() && n != 0 {
		return fmt.Errorf("update touched %d rows", n)
	}
	return nil
}

func (c *Conn) DeleteRow(ctx context.Context, schema, table string, pk []string, res *Result, row []Value) error {
	if len(pk) == 0 {
		return fmt.Errorf("%s has no primary key; delete with a query", table)
	}
	where, args := c.whereArgs(res, row, pk, nil)
	_, err := c.DB.ExecContext(ctx, "DELETE FROM "+c.QT(schema, table)+" WHERE "+where, args...)
	return err
}

// InsertRow inserts the given columns; omitted columns take their defaults.
func (c *Conn) InsertRow(ctx context.Context, schema, table string, cols []string, vals []Value) error {
	if len(cols) == 0 {
		_, err := c.DB.ExecContext(ctx, "INSERT INTO "+c.QT(schema, table)+" DEFAULT VALUES")
		return err
	}
	qs, phs, args := make([]string, len(cols)), make([]string, len(cols)), make([]any, len(cols))
	for i := range cols {
		qs[i], phs[i], args[i] = c.Q(cols[i]), c.ph(i), vals[i].arg()
	}
	_, err := c.DB.ExecContext(ctx, "INSERT INTO "+c.QT(schema, table)+" ("+strings.Join(qs, ", ")+") VALUES ("+strings.Join(phs, ", ")+")", args...)
	return err
}
