package ui

import (
	"context"
	"fmt"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/hussaratkuro/hyprdb/internal/db"
)

type tab int

const (
	tabData tab = iota
	tabColumns
	tabIndexes
	tabInfo
	tabQuery
	tabServer
	tabTransfer
	tabCount
)

var tabDefs = [tabCount]struct{ ico, label string }{
	{icoTable, "Data"}, {icoColumns, "Columns"}, {icoList, "Indexes"}, {icoInfo, "Info"}, {icoPlay, "Query"}, {icoServer, "Server"}, {icoDownload, "Import / Export"},
}

type workState struct {
	conn    *db.Conn
	version string
	schemas []string
	open    string // expanded schema
	tables  map[string][]db.TableInfo
	clicks  map[string]*widget.Clickable
	side    widget.List
	filter  widget.Editor
	tab     tab
	tabBtns [tabCount]widget.Clickable
	refresh widget.Clickable

	// current table
	schema, table string
	info          db.TableInfo
	cols          []db.Column
	pk            []string
	idx           []db.Index
	ddl           string
	ddlSel        widget.Selectable
	engines       []string
	engineBtns    []widget.Clickable
	detail        widget.List

	grid   grid // data tab
	data   dataState
	query  queryState
	server grid
	xfer   xferState
}

func (a *App) initWork() {
	w := &a.work
	w.side.Axis, w.detail.Axis = layout.Vertical, layout.Vertical
	w.filter = newEditor(true, false, 0)
	w.clicks = map[string]*widget.Clickable{}
	w.grid, w.server = newGrid(), newGrid()
	a.initData()
	a.initQuery()
	a.initXfer()
}

func (a *App) click(key string) *widget.Clickable {
	c := a.work.clicks[key]
	if c == nil {
		c = new(widget.Clickable)
		a.work.clicks[key] = c
	}
	return c
}

func (a *App) enterWork(conn *db.Conn) {
	w := &a.work
	if w.conn != nil {
		w.conn.Close()
	}
	w.conn, w.version = conn, conn.Version()
	w.schemas, w.tables, w.open = nil, map[string][]db.TableInfo{}, ""
	w.schema, w.table, w.cols, w.pk, w.idx, w.ddl, w.engines = "", "", nil, nil, nil, "", nil
	w.grid.set(nil)
	w.server.set(nil)
	w.query.grid.set(nil)
	w.tab = tabData
	a.screen = screenWork
	a.loadSchemas(conn.Cfg.Database)
}

func (a *App) leaveWork() {
	if a.work.conn != nil {
		a.work.conn.Close()
		a.work.conn = nil
	}
	a.screen = screenHome
}

func (a *App) loadSchemas(open string) {
	w := &a.work
	conn := w.conn
	a.run("loading schemas", func(ctx context.Context) (string, error) {
		s, err := conn.Schemas()
		if err != nil {
			return "", err
		}
		a.post(func() {
			w.schemas = s
			if open == "" && !conn.IsMySQL() {
				open = "public"
			}
			for _, x := range s {
				if x == open {
					a.openSchema(x)
				}
			}
		})
		return "", nil
	}, nil)
}

func (a *App) openSchema(schema string) {
	w := &a.work
	conn := w.conn
	w.open = schema
	a.run("listing tables of "+schema, func(ctx context.Context) (string, error) {
		t, err := conn.Tables(schema)
		if err != nil {
			return "", err
		}
		a.post(func() { w.tables[schema] = t })
		return "", nil
	}, nil)
}

func (a *App) openTable(schema string, t db.TableInfo) {
	w := &a.work
	conn := w.conn
	w.schema, w.table, w.info = schema, t.Name, t
	w.cols, w.pk, w.idx, w.ddl = nil, nil, nil, ""
	w.grid.set(nil)
	w.data.page, w.data.total = 0, 0
	w.data.where.SetText("")
	a.run("inspecting "+t.Name, func(ctx context.Context) (string, error) {
		cols, err := conn.Columns(schema, t.Name)
		if err != nil {
			return "", err
		}
		idx, _ := conn.Indexes(schema, t.Name)
		ddl, _ := conn.CreateSQL(ctx, schema, t.Name)
		var engines []string
		if conn.IsMySQL() && w.engines == nil {
			engines, _ = conn.Engines()
		}
		a.post(func() {
			if w.schema != schema || w.table != t.Name {
				return
			}
			w.cols, w.pk, w.idx, w.ddl = cols, db.PrimaryKey(cols), idx, ddl
			if engines != nil {
				w.engines, w.engineBtns = engines, make([]widget.Clickable, len(engines))
			}
			a.loadPage()
		})
		return "", nil
	}, nil)
}

func (a *App) refreshWork() {
	w := &a.work
	if w.table != "" {
		a.openSchema(w.schema)
		a.openTable(w.schema, w.info)
		return
	}
	a.loadSchemas(w.open)
}

func (a *App) layoutWork(gtx C) D {
	w := &a.work
	if a.backBtn.Clicked(gtx) {
		a.leaveWork()
		return D{}
	}
	if w.refresh.Clicked(gtx) {
		a.refreshWork()
	}
	for i := range w.tabBtns {
		if w.tabBtns[i].Clicked(gtx) {
			w.tab = tab(i)
			if w.tab == tabServer && w.server.res == nil {
				a.loadServer()
			}
		}
	}
	cfg := w.conn.Cfg
	sub := cfg.Kind.Label() + " " + w.version + " · " + cfg.User + "@" + cfg.Addr()
	return a.page(gtx, func(gtx C) D {
		return a.header(gtx, cfg.Name, sub, true, a.iconBtn(&w.refresh, icoRefresh))
	}, func(gtx C) D {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.X = gtx.Dp(260)
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return a.sidebar(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(a.tabBar),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Flexed(1, func(gtx C) D {
						gtx.Constraints.Min = gtx.Constraints.Max
						switch w.tab {
						case tabColumns:
							return a.layoutColumns(gtx)
						case tabIndexes:
							return a.layoutIndexes(gtx)
						case tabInfo:
							return a.layoutInfo(gtx)
						case tabQuery:
							return a.layoutQuery(gtx)
						case tabServer:
							return a.layoutGrid(gtx, &w.server)
						case tabTransfer:
							return a.layoutXfer(gtx)
						}
						return a.layoutData(gtx)
					}),
				)
			}),
		)
	})
}

func (a *App) tabBar(gtx C) D {
	w := &a.work
	ws := []layout.Widget{}
	for i := range tabDefs {
		i := i
		t := tab(i)
		if t <= tabInfo && w.table == "" {
			continue
		}
		if t == tabServer && !w.conn.IsMySQL() {
			tabDefs[i].label = "Settings"
		}
		ws = append(ws, a.tabBtn(&w.tabBtns[i], tabDefs[i].ico, tabDefs[i].label, w.tab == t))
	}
	if w.table != "" {
		ws = append(ws, layout.Spacer{Width: unit.Dp(8)}.Layout, a.mono(w.schema+"."+w.table, a.pal.Yellow, unit.Sp(13)))
		if len(w.pk) == 0 && w.cols != nil {
			ws = append(ws, a.pill("no primary key: read only", a.pal.Peach))
		}
	}
	return a.row(ws...)(gtx)
}

func (a *App) tabBtn(c *widget.Clickable, ico, label string, on bool) layout.Widget {
	return func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.choice(c, ico+"  "+label, on)),
		)
	}
}

// ---- sidebar: schemas and tables ----

func (a *App) sidebar(gtx C) D {
	w := &a.work
	q := strings.ToLower(strings.TrimSpace(w.filter.Text()))
	type entry struct {
		schema string
		table  *db.TableInfo
	}
	var entries []entry
	for i := range w.schemas {
		s := w.schemas[i]
		entries = append(entries, entry{schema: s})
		if s != w.open {
			continue
		}
		ts := w.tables[s]
		for j := range ts {
			if q == "" || strings.Contains(strings.ToLower(ts[j].Name), q) {
				entries = append(entries, entry{schema: s, table: &ts[j]})
			}
		}
	}
	for _, e := range entries {
		key := e.schema
		if e.table != nil {
			key += "." + e.table.Name
		}
		if a.click(key).Clicked(gtx) {
			if e.table != nil {
				a.openTable(e.schema, *e.table)
			} else if w.open == e.schema {
				w.open = ""
			} else {
				a.openSchema(e.schema)
			}
		}
	}
	gtx.Constraints.Min = gtx.Constraints.Max
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(a.editor(&w.filter, icoSearch+"  filter tables", gtx.Focused(&w.filter))),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx C) D {
			ls := material.List(a.th, &w.side)
			ls.Indicator.Color = a.pal.Surface2
			return ls.Layout(gtx, len(entries), func(gtx C, i int) D {
				e := entries[i]
				if e.table == nil {
					ico := icoRight
					if e.schema == w.open {
						ico = icoDown
					}
					return a.listRow(a.click(e.schema), false, a.row(a.icon(ico, a.pal.Overlay1, unit.Sp(11)), a.icon(icoDB, a.pal.Mauve, unit.Sp(13)), func(gtx C) D {
						l := material.Label(a.th, unit.Sp(13), e.schema)
						l.Color, l.Font.Weight, l.MaxLines = a.pal.Text, font.SemiBold, 1
						return l.Layout(gtx)
					}))(gtx)
				}
				ico, col := icoTable, a.pal.Subtext1
				if e.table.Type != "TABLE" {
					ico, col = icoEye, a.pal.Sapphire
				}
				sel := w.schema == e.schema && w.table == e.table.Name
				return layout.Inset{Left: unit.Dp(18)}.Layout(gtx, a.listRow(a.click(e.schema+"."+e.table.Name), sel, a.row(a.icon(ico, col, unit.Sp(12)), func(gtx C) D {
					l := material.Label(a.th, unit.Sp(13), e.table.Name)
					l.Color, l.MaxLines = a.pal.Text, 1
					return l.Layout(gtx)
				})))
			})
		}),
	)
}

// ---- columns / indexes / info ----

func (a *App) kvRows(gtx C, head []string, rows [][]string) D {
	w := &a.work
	ls := material.List(a.th, &w.detail)
	ls.Indicator.Color = a.pal.Surface2
	widths := make([]float32, len(head))
	for i := range head {
		widths[i] = 1
	}
	widths[0] = 1.3
	line := func(cells []string, bold bool, colr func(int) layout.Widget) layout.Widget {
		return func(gtx C) D {
			children := make([]layout.FlexChild, len(cells))
			for i := range cells {
				i := i
				children[i] = layout.Flexed(widths[i], func(gtx C) D {
					l := material.Label(a.th, unit.Sp(13), cells[i])
					l.Color, l.MaxLines = a.pal.Text, 1
					if bold {
						l.Color, l.Font.Weight = a.pal.Subtext1, font.SemiBold
					} else if i == 0 {
						l.Color = a.pal.Yellow
					}
					return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Right: unit.Dp(8)}.Layout(gtx, l.Layout)
				})
			}
			return layout.Flex{}.Layout(gtx, children...)
		}
	}
	return ls.Layout(gtx, len(rows)+1, func(gtx C, i int) D {
		if i == 0 {
			return line(head, true, nil)(gtx)
		}
		return line(rows[i-1], false, nil)(gtx)
	})
}

func (a *App) layoutColumns(gtx C) D {
	w := &a.work
	rows := make([][]string, len(w.cols))
	for i, c := range w.cols {
		null := "NOT NULL"
		if c.Nullable {
			null = "NULL"
		}
		rows[i] = []string{c.Name, c.Type, null, c.Key, c.Default, c.Extra, c.Comment}
	}
	return a.kvRows(gtx, []string{"Column", "Type", "Nullable", "Key", "Default", "Extra", "Comment"}, rows)
}

func (a *App) layoutIndexes(gtx C) D {
	w := &a.work
	rows := make([][]string, len(w.idx))
	for i, ix := range w.idx {
		u := ""
		if ix.Unique {
			u = "UNIQUE"
		}
		rows[i] = []string{ix.Name, u, ix.Type, ix.Columns}
	}
	return a.kvRows(gtx, []string{"Index", "Unique", "Type", "Columns / definition"}, rows)
}

func (a *App) layoutInfo(gtx C) D {
	w := &a.work
	t := w.info
	kv := func(k, v string) layout.Widget {
		return a.row(func(gtx C) D { gtx.Constraints.Min.X = gtx.Dp(120); return a.small(k, a.pal.Overlay1)(gtx) }, a.mono(v, a.pal.Rosewater, unit.Sp(13)))
	}
	ws := []layout.Widget{kv("Type", t.Type), kv("Rows (approx)", fmt.Sprint(t.Rows)), kv("Data size", human(t.DataSize)), kv("Index size", human(t.IndexSize))}
	if w.conn.IsMySQL() {
		for i := range w.engineBtns {
			if w.engineBtns[i].Clicked(gtx) && w.engines[i] != t.Engine {
				a.changeEngine(w.engines[i])
			}
		}
		ws = append(ws, kv("Engine", t.Engine), kv("Collation", t.Collation), kv("Auto increment", fmt.Sprint(t.AutoInc)), kv("Created", t.Created), kv("Updated", t.Updated))
		if t.Type == "TABLE" && len(w.engines) > 0 {
			pills := []layout.Widget{a.small("Change engine:", a.pal.Overlay1)}
			for i, e := range w.engines {
				pills = append(pills, a.choice(&w.engineBtns[i], e, e == t.Engine))
			}
			ws = append(ws, a.row(pills...))
		}
	}
	if t.Comment != "" {
		ws = append(ws, kv("Comment", t.Comment))
	}
	ws = append(ws, a.small("DDL", a.pal.Overlay1), func(gtx C) D {
		return a.box(gtx, a.pal.Surface0, a.pal.Surface1, unit.Dp(8), layout.UniformInset(unit.Dp(10)), func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			l := material.Label(a.th, unit.Sp(12), w.ddl)
			l.Color, l.State = a.pal.Text, &w.ddlSel
			return l.Layout(gtx)
		})
	})
	ls := material.List(a.th, &w.detail)
	ls.Indicator.Color = a.pal.Surface2
	return ls.Layout(gtx, 1, func(gtx C, _ int) D { return a.column(gtx, ws...) })
}

func (a *App) changeEngine(engine string) {
	w := &a.work
	conn, schema, table := w.conn, w.schema, w.table
	a.confirm("Change storage engine", fmt.Sprintf("ALTER TABLE %s ENGINE=%s rebuilds the table. Continue?", table, engine), func() {
		a.run("altering "+table, func(ctx context.Context) (string, error) {
			if err := conn.AlterEngine(schema, table, engine); err != nil {
				return "", err
			}
			a.post(a.refreshWork)
			return table + " now uses " + engine, nil
		}, nil)
	})
}

func (a *App) loadServer() {
	w := &a.work
	conn := w.conn
	a.run("querying server", func(ctx context.Context) (string, error) {
		res, err := conn.Query(ctx, conn.ServerQuery())
		if err != nil {
			return "", err
		}
		a.post(func() { w.server.set(res) })
		return "", nil
	}, nil)
}

func human(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GiB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	}
	return fmt.Sprintf("%d B", n)
}
