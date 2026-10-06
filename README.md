# hyprdb

A small DBeaver-like desktop client for MySQL, MariaDB and PostgreSQL,
written in Go with [Gio](https://gioui.org). It follows the HyDE / Wallbash
palette like the other hypr* tools.

- saved connections in `~/.config/hyprdb/connections.json`; passwords are
  never stored: a connection names a hyprvault / gopass entry (resolved with
  `hyprvault credential get`) or the password is asked at connect time
- browse databases / schemas, tables and views, columns, indexes, table
  status (engine, sizes, collation, auto increment) and the DDL
- MySQL / MariaDB: storage engine per table, change it with `ALTER TABLE`,
  `SHOW ENGINES` on the Server tab; PostgreSQL: `pg_settings`
- data grid with paging and a `WHERE` filter; double-click a cell to edit it
  (`UPDATE` by primary key), insert and delete rows
- query editor (Ctrl+Enter runs the whole text or the selection)
- export tables or a whole schema as SQL (phpMyAdmin-style options: CREATE TABLE,
  DROP TABLE before it, INSERT data; to a file or as copyable text) and import
  `.sql` files or pasted SQL
  without `mysqldump` / `pg_dump` / `psql`

## Build

```sh
./install-local.sh      # go build -tags novulkan, installs binary, icon and .desktop
```

Dependencies: Go 1.27, `gioui.org`, `github.com/go-sql-driver/mysql`,
`github.com/jackc/pgx/v5`; at runtime `hyprvault` or `gopass` in `PATH` for
vault credentials.

## Keys

| key | action |
| --- | --- |
| Ctrl+Enter | run query |
| Ctrl+R | refresh schema / table |
| Ctrl+N | new connection (home screen) |
| Ctrl+←/→, Ctrl+Shift+←/→, Ctrl+Backspace/Delete | word movement, selection, deletion in every text field |
| Esc | close dialog, cancel cell edit |
| Ctrl+Q | quit |

## Security notes

The vault password is kept in memory for the session only. Ad-hoc passwords
are used once and discarded. Exports are plain SQL text files; mind where you
write them.
