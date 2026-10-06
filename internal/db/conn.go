package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const timeout = 10 * time.Second

type Conn struct {
	Cfg Config
	DB  *sql.DB
}

func (c *Conn) IsMySQL() bool { return c.Cfg.Kind != Postgres }

// Open dials the server and verifies the login with a ping.
func Open(cfg Config, password string) (*Conn, error) {
	var (
		db  *sql.DB
		err error
	)
	if cfg.Kind == Postgres {
		esc := func(s string) string { return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'" }
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=prefer connect_timeout=10",
			esc(cfg.Host), cfg.Port, esc(cfg.User), esc(password), esc(cfg.Database))
		db, err = sql.Open("pgx", dsn)
	} else {
		mc := mysql.NewConfig()
		mc.User, mc.Passwd, mc.Net, mc.Addr, mc.DBName = cfg.User, password, "tcp", cfg.Addr(), cfg.Database
		mc.Timeout, mc.ReadTimeout = timeout, 5*time.Minute
		mc.AllowNativePasswords = true
		mc.Params = map[string]string{"charset": "utf8mb4"}
		db, err = sql.Open("mysql", mc.FormatDSN())
	}
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one session so USE / search_path stick
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Conn{Cfg: cfg, DB: db}, nil
}

func (c *Conn) Close() { c.DB.Close() }

// Q quotes an identifier for the connection's dialect.
func (c *Conn) Q(name string) string {
	if c.IsMySQL() {
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (c *Conn) QT(schema, table string) string { return c.Q(schema) + "." + c.Q(table) }

// ph is the i-th (0-based) bind placeholder.
func (c *Conn) ph(i int) string {
	if c.IsMySQL() {
		return "?"
	}
	return fmt.Sprintf("$%d", i+1)
}

func (c *Conn) Version() string {
	var v string
	_ = c.DB.QueryRow("SELECT VERSION()").Scan(&v)
	return v
}

// Schemas lists databases (MySQL) or schemas of the connected database (PostgreSQL).
func (c *Conn) Schemas() ([]string, error) {
	q := "SHOW DATABASES"
	if !c.IsMySQL() {
		q = "SELECT nspname FROM pg_namespace WHERE nspname NOT LIKE 'pg\\_%' AND nspname <> 'information_schema' ORDER BY nspname"
	}
	rows, err := c.DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type TableInfo struct {
	Name, Type, Engine, Collation, Comment, Created, Updated string
	Rows, DataSize, IndexSize, AutoInc                       int64
}

func (c *Conn) Tables(schema string) ([]TableInfo, error) {
	if !c.IsMySQL() {
		rows, err := c.DB.Query(`SELECT c.relname, CASE c.relkind WHEN 'v' THEN 'VIEW' WHEN 'm' THEN 'MATVIEW' ELSE 'TABLE' END,
			GREATEST(c.reltuples::bigint, 0), pg_relation_size(c.oid), pg_indexes_size(c.oid), COALESCE(obj_description(c.oid, 'pg_class'), '')
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind IN ('r','v','m','p') ORDER BY c.relname`, schema)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []TableInfo
		for rows.Next() {
			var t TableInfo
			if err := rows.Scan(&t.Name, &t.Type, &t.Rows, &t.DataSize, &t.IndexSize, &t.Comment); err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, rows.Err()
	}
	res, err := c.Query(context.Background(), "SHOW TABLE STATUS FROM "+c.Q(schema))
	if err != nil {
		return nil, err
	}
	var out []TableInfo
	for _, r := range res.Rows {
		g := func(col string) string { return res.Get(r, col) }
		t := TableInfo{Name: g("Name"), Type: "TABLE", Engine: g("Engine"), Collation: g("Collation"), Comment: g("Comment"), Created: g("Create_time"), Updated: g("Update_time")}
		if t.Engine == "" {
			t.Type = "VIEW"
		}
		fmt.Sscan(g("Rows"), &t.Rows)
		fmt.Sscan(g("Data_length"), &t.DataSize)
		fmt.Sscan(g("Index_length"), &t.IndexSize)
		fmt.Sscan(g("Auto_increment"), &t.AutoInc)
		out = append(out, t)
	}
	return out, nil
}

type Column struct {
	Name, Type, Key, Default, Extra, Comment string
	Nullable                                 bool
}

func (c *Conn) Columns(schema, table string) ([]Column, error) {
	var rows *sql.Rows
	var err error
	if c.IsMySQL() {
		rows, err = c.DB.Query(`SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE = 'YES', COLUMN_KEY, COALESCE(COLUMN_DEFAULT, ''), EXTRA, COLUMN_COMMENT
			FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position`, schema, table)
	} else {
		rows, err = c.DB.Query(`SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
			CASE WHEN EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisprimary AND a.attnum = ANY(i.indkey)) THEN 'PRI' ELSE '' END,
			COALESCE(pg_get_expr(d.adbin, d.adrelid), ''), CASE WHEN a.attidentity <> '' THEN 'identity' WHEN a.attgenerated <> '' THEN 'generated' ELSE '' END,
			COALESCE(col_description(a.attrelid, a.attnum), '')
			FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			WHERE a.attrelid = $1::regclass AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`, c.QT(schema, table))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var col Column
		if err := rows.Scan(&col.Name, &col.Type, &col.Nullable, &col.Key, &col.Default, &col.Extra, &col.Comment); err != nil {
			return nil, err
		}
		out = append(out, col)
	}
	return out, rows.Err()
}

func PrimaryKey(cols []Column) []string {
	var pk []string
	for _, c := range cols {
		if c.Key == "PRI" {
			pk = append(pk, c.Name)
		}
	}
	return pk
}

type Index struct {
	Name, Columns, Type string
	Unique              bool
}

func (c *Conn) Indexes(schema, table string) ([]Index, error) {
	if !c.IsMySQL() {
		rows, err := c.DB.Query(`SELECT indexname, indexdef, indexdef LIKE 'CREATE UNIQUE%' FROM pg_indexes WHERE schemaname = $1 AND tablename = $2 ORDER BY indexname`, schema, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []Index
		for rows.Next() {
			var ix Index
			if err := rows.Scan(&ix.Name, &ix.Columns, &ix.Unique); err != nil {
				return nil, err
			}
			out = append(out, ix)
		}
		return out, rows.Err()
	}
	res, err := c.Query(context.Background(), "SHOW INDEX FROM "+c.QT(schema, table))
	if err != nil {
		return nil, err
	}
	var out []Index
	byName := map[string]int{}
	for _, r := range res.Rows {
		name := res.Get(r, "Key_name")
		i, ok := byName[name]
		if !ok {
			i = len(out)
			byName[name] = i
			out = append(out, Index{Name: name, Unique: res.Get(r, "Non_unique") == "0", Type: res.Get(r, "Index_type")})
		}
		if out[i].Columns != "" {
			out[i].Columns += ", "
		}
		out[i].Columns += res.Get(r, "Column_name")
	}
	return out, nil
}

// Engines returns SHOW ENGINES (MySQL/MariaDB only).
func (c *Conn) Engines() ([]string, error) {
	res, err := c.Query(context.Background(), "SHOW ENGINES")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range res.Rows {
		if s := res.Get(r, "Support"); s == "YES" || s == "DEFAULT" {
			out = append(out, res.Get(r, "Engine"))
		}
	}
	return out, nil
}

func (c *Conn) AlterEngine(schema, table, engine string) error {
	if !c.IsMySQL() {
		return fmt.Errorf("storage engines exist only on MySQL/MariaDB")
	}
	_, err := c.DB.Exec("ALTER TABLE " + c.QT(schema, table) + " ENGINE=" + c.Q(engine))
	return err
}

// ServerQuery is the "Server" tab: engines for MySQL, settings for PostgreSQL.
func (c *Conn) ServerQuery() string {
	if c.IsMySQL() {
		return "SHOW ENGINES"
	}
	return "SELECT name, setting, unit, short_desc FROM pg_settings ORDER BY name"
}

// AddColumn runs ALTER TABLE … ADD COLUMN. after is "" (end), "FIRST" or a
// column name; PostgreSQL cannot position columns and ignores it. The
// default is taken verbatim when it looks like a number, NULL or a function
// call, otherwise it is quoted as a string.
func (c *Conn) AddColumn(ctx context.Context, schema, table string, col Column, after string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "ALTER TABLE %s ADD COLUMN %s %s", c.QT(schema, table), c.Q(col.Name), strings.TrimSpace(col.Type))
	if !col.Nullable {
		b.WriteString(" NOT NULL")
	}
	if d := strings.TrimSpace(col.Default); d != "" {
		b.WriteString(" DEFAULT " + c.defaultExpr(d))
	}
	if c.IsMySQL() {
		if col.Comment != "" {
			b.WriteString(" COMMENT " + c.literal(Value{Text: col.Comment}, ""))
		}
		switch {
		case strings.EqualFold(after, "FIRST"):
			b.WriteString(" FIRST")
		case after != "":
			b.WriteString(" AFTER " + c.Q(after))
		}
	}
	_, err := c.DB.ExecContext(ctx, b.String())
	return err
}

func (c *Conn) defaultExpr(d string) string {
	u := strings.ToUpper(d)
	if u == "NULL" || u == "TRUE" || u == "FALSE" || strings.HasSuffix(u, ")") || strings.HasSuffix(u, "()") {
		return d
	}
	if _, err := fmt.Sscanf(d, "%g", new(float64)); err == nil && strings.Trim(d, "0123456789.+-eE") == "" {
		return d
	}
	if u == "CURRENT_TIMESTAMP" || u == "CURRENT_DATE" || u == "NOW" {
		return u
	}
	return c.literal(Value{Text: d}, "")
}
