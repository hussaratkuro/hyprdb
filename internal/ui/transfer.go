package ui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/hussaratkuro/hyprdb/internal/db"
)

type xferState struct {
	exportPath, importPath      widget.Editor
	scopeTable, scopeSchema     widget.Clickable
	wholeSchema                 bool
	optCreate, optDrop, optData widget.Clickable
	opt                         db.ExportOptions
	toFile, toText              widget.Clickable
	asText                      bool
	out                         widget.Editor // read-only export text
	copyBtn                     widget.Clickable
	importText                  widget.Editor
	exportBtn, importBtn        widget.Clickable
	importTextBtn               widget.Clickable
	browseExport, browseImport  widget.Clickable
	progress                    string
	list                        widget.List
}

func (a *App) initXfer() {
	x := &a.work.xfer
	x.exportPath, x.importPath = newEditor(true, true, 0), newEditor(true, true, 0)
	x.out = widget.Editor{ReadOnly: true}
	x.opt = db.ExportOptions{Create: true, Drop: true, Data: true}
	x.list.Axis = layout.Vertical
	home, _ := os.UserHomeDir()
	x.exportPath.SetText(filepath.Join(home, "dump.sql"))
}

func (a *App) toggle(c *widget.Clickable, label string, on *bool) layout.Widget {
	if c.Clicked(a.gtx) {
		*on = !*on
	}
	ico := icoSquare
	if *on {
		ico = icoChecked
	}
	return a.choice(c, ico+"  "+label, *on)
}

func (a *App) layoutXfer(gtx C) D {
	w := &a.work
	x := &w.xfer
	a.gtx = gtx
	if x.scopeTable.Clicked(gtx) {
		x.wholeSchema = false
	}
	if x.scopeSchema.Clicked(gtx) {
		x.wholeSchema = true
	}
	if x.toFile.Clicked(gtx) {
		x.asText = false
	}
	if x.toText.Clicked(gtx) {
		x.asText = true
	}
	if x.copyBtn.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{Data: io.NopCloser(strings.NewReader(x.out.Text()))})
		a.setStatus("export copied to clipboard", false)
	}
	if x.importTextBtn.Clicked(gtx) {
		a.importReader("pasted SQL", strings.NewReader(x.importText.Text()))
	}
	if x.exportBtn.Clicked(gtx) || submitted(gtx, &x.exportPath) {
		a.export()
	}
	if x.importBtn.Clicked(gtx) || submitted(gtx, &x.importPath) {
		a.importFile()
	}
	if x.browseExport.Clicked(gtx) {
		a.openBrowse(&x.exportPath, true)
	}
	if x.browseImport.Clicked(gtx) {
		a.openBrowse(&x.importPath, false)
	}
	schema := w.open
	if w.schema != "" {
		schema = w.schema
	}
	pathRow := func(label string, ed *widget.Editor, browse *widget.Clickable) layout.Widget {
		return func(gtx C) D {
			return layout.Flex{Alignment: layout.End}.Layout(gtx,
				layout.Flexed(1, a.field(label, ed, "/path/file.sql", gtx.Focused(ed))),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(a.btn(browse, icoFolder, "Browse", btnSecondary)),
			)
		}
	}
	scopeTable := "current table"
	if w.table != "" {
		scopeTable += " (" + w.table + ")"
	}
	textBox := func(ed *widget.Editor, hint string, h unit.Dp) layout.Widget {
		return func(gtx C) D {
			gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(h), gtx.Dp(h)
			editKeys(gtx, ed)
			return a.box(gtx, a.pal.Surface0, a.pal.Surface1, unit.Dp(8), layout.UniformInset(unit.Dp(10)), func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				e := material.Editor(a.th, ed, hint)
				e.Color, e.HintColor, e.TextSize = a.pal.Text, a.pal.Overlay0, unit.Sp(12)
				e.SelectionColor = alpha(a.pal.Mauve, 0x66)
				return e.Layout(gtx)
			})
		}
	}
	ws := []layout.Widget{
		a.row(a.iconDisc(icoDownload, a.pal.Mauve, 30), a.heading("Export")),
		a.small("SQL for the tables of "+schema+" (views are skipped), written by hyprdb itself: no mysqldump / pg_dump needed.", a.pal.Subtext0),
		a.row(a.choice(&x.scopeTable, scopeTable, !x.wholeSchema), a.choice(&x.scopeSchema, "whole schema "+schema, x.wholeSchema)),
		a.row(a.toggle(&x.optCreate, "CREATE TABLE", &x.opt.Create), a.toggle(&x.optDrop, "DROP TABLE before it", &x.opt.Drop), a.toggle(&x.optData, "INSERT data", &x.opt.Data)),
		a.row(a.small("Output:", a.pal.Overlay1), a.choice(&x.toFile, "save as file", !x.asText), a.choice(&x.toText, "view as text", x.asText)),
	}
	if x.asText {
		ws = append(ws, a.row(a.btn(&x.exportBtn, icoDownload, "Export", btnPrimary), a.btn(&x.copyBtn, icoFile, "Copy", btnSecondary), a.small(x.progress, a.pal.Peach)),
			textBox(&x.out, "export output appears here", 260))
	} else {
		ws = append(ws, pathRow("Output file", &x.exportPath, &x.browseExport),
			a.row(a.btn(&x.exportBtn, icoDownload, "Export", btnPrimary), a.small(x.progress, a.pal.Peach)))
	}
	ws = append(ws,
		layout.Spacer{Height: unit.Dp(16)}.Layout,
		a.row(a.iconDisc(icoUpload, a.pal.Peach, 30), a.heading("Import")),
		a.small("Runs every statement in order on this connection (schema "+schema+" selected first) and stops at the first error. From a .sql file or pasted text.", a.pal.Subtext0),
		pathRow("Script file", &x.importPath, &x.browseImport),
		a.row(a.btn(&x.importBtn, icoUpload, "Import file", btnPrimary)),
		textBox(&x.importText, "…or paste SQL here", 180),
		a.row(a.btn(&x.importTextBtn, icoPlay, "Run pasted SQL", btnPrimary)),
	)
	ls := material.List(a.th, &x.list)
	ls.Indicator.Color = a.pal.Surface2
	return ls.Layout(gtx, 1, func(gtx C, _ int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return a.column(gtx, ws...)
	})
}

func (a *App) selectSchema(ctx context.Context) {
	w := &a.work
	schema := w.open
	if w.schema != "" {
		schema = w.schema
	}
	if schema == "" {
		return
	}
	if w.conn.IsMySQL() {
		w.conn.DB.ExecContext(ctx, "USE "+w.conn.Q(schema))
	} else {
		w.conn.DB.ExecContext(ctx, "SET search_path TO "+w.conn.Q(schema)+", public")
	}
}

func (a *App) export() {
	w := &a.work
	x := &w.xfer
	conn := w.conn
	schema := w.open
	if w.schema != "" {
		schema = w.schema
	}
	var tables []string
	if x.wholeSchema {
		for _, t := range w.tables[schema] {
			if t.Type == "TABLE" {
				tables = append(tables, t.Name)
			}
		}
	} else if w.table != "" {
		tables = []string{w.table}
	}
	if len(tables) == 0 {
		a.setStatus("nothing to export: select a table or a schema with tables", true)
		return
	}
	if !x.opt.Create && !x.opt.Data {
		a.setStatus("pick CREATE TABLE and/or INSERT data", true)
		return
	}
	opt, progress := x.opt, func(s string) { a.post(func() { x.progress = s }) }
	if x.asText {
		a.run("exporting", func(ctx context.Context) (string, error) {
			var buf bytes.Buffer
			if err := conn.Export(ctx, schema, tables, opt, &buf, progress); err != nil {
				return "", err
			}
			a.post(func() { x.progress = ""; x.out.SetText(buf.String()) })
			return fmt.Sprintf("exported %d tables (%s)", len(tables), human(int64(buf.Len()))), nil
		}, nil)
		return
	}
	path := strings.TrimSpace(x.exportPath.Text())
	if path == "" {
		a.setStatus("output file is required", true)
		return
	}
	a.run("exporting", func(ctx context.Context) (string, error) {
		f, err := os.Create(path)
		if err != nil {
			return "", err
		}
		err = conn.Export(ctx, schema, tables, opt, f, progress)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", err
		}
		a.post(func() { x.progress = "" })
		return fmt.Sprintf("exported %d tables to %s", len(tables), path), nil
	}, nil)
}

func (a *App) importFile() {
	path := strings.TrimSpace(a.work.xfer.importPath.Text())
	if path == "" {
		a.setStatus("script file is required", true)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		a.setStatus(err.Error(), true)
		return
	}
	a.importReader(filepath.Base(path), f)
}

// importReader runs the script from r (closed afterwards if it is a Closer).
func (a *App) importReader(name string, r io.Reader) {
	w := &a.work
	x := &w.xfer
	conn := w.conn
	a.run("importing", func(ctx context.Context) (string, error) {
		if c, ok := r.(io.Closer); ok {
			defer c.Close()
		}
		a.selectSchema(ctx)
		n, err := conn.Import(ctx, r, func(n int) {
			if n%20 == 0 {
				a.post(func() { x.progress = fmt.Sprintf("%d statements", n) })
			}
		})
		a.post(func() {
			x.progress = ""
			a.refreshWork()
		})
		if err != nil {
			return "", fmt.Errorf("after %d statements: %w", n, err)
		}
		return fmt.Sprintf("imported %d statements from %s", n, name), nil
	}, nil)
}

// ---- in-app file browser (no portal/zenity on this box) ----

type browseState struct {
	target      *widget.Editor
	saving      bool // picking an output: directories step in, "Use" picks dir + name
	dir         string
	entries     []os.DirEntry
	clicks      []widget.Clickable
	list        widget.List
	name        widget.Editor
	up, use, no widget.Clickable
	hidden      widget.Clickable
	showHidden  bool
}

func (a *App) openBrowse(target *widget.Editor, saving bool) {
	b := &a.fb
	b.target, b.saving = target, saving
	b.list.Axis = layout.Vertical
	b.name = newEditor(true, true, 0)
	start := strings.TrimSpace(target.Text())
	if start == "" {
		start, _ = os.UserHomeDir()
	}
	if info, err := os.Stat(start); err != nil || !info.IsDir() {
		b.name.SetText(filepath.Base(start))
		start = filepath.Dir(start)
	}
	a.loadBrowse(start)
	a.modal = modalBrowse
}

func (a *App) loadBrowse(dir string) {
	b := &a.fb
	entries, err := os.ReadDir(dir)
	if err != nil {
		a.setStatus(err.Error(), true)
		return
	}
	var keep []os.DirEntry
	for _, e := range entries {
		if b.showHidden || !strings.HasPrefix(e.Name(), ".") {
			keep = append(keep, e)
		}
	}
	sort.Slice(keep, func(i, j int) bool {
		if keep[i].IsDir() != keep[j].IsDir() {
			return keep[i].IsDir()
		}
		return strings.ToLower(keep[i].Name()) < strings.ToLower(keep[j].Name())
	})
	b.dir, b.entries, b.clicks = filepath.Clean(dir), keep, make([]widget.Clickable, len(keep))
	b.list.Position = layout.Position{}
}

func (a *App) layoutBrowse(gtx C) D {
	b := &a.fb
	if b.up.Clicked(gtx) {
		a.loadBrowse(filepath.Dir(b.dir))
	}
	if b.hidden.Clicked(gtx) {
		b.showHidden = !b.showHidden
		a.loadBrowse(b.dir)
	}
	if b.no.Clicked(gtx) {
		a.modal = modalNone
	}
	if b.use.Clicked(gtx) || submitted(gtx, &b.name) {
		if n := strings.TrimSpace(b.name.Text()); n != "" {
			b.target.SetText(filepath.Join(b.dir, n))
			a.modal = modalNone
		}
	}
	for i, e := range b.entries {
		if b.clicks[i].Clicked(gtx) {
			if e.IsDir() {
				a.loadBrowse(filepath.Join(b.dir, e.Name()))
			} else if b.saving {
				b.name.SetText(e.Name())
			} else {
				b.target.SetText(filepath.Join(b.dir, e.Name()))
				a.modal = modalNone
			}
		}
	}
	title := "Choose a .sql file"
	if b.saving {
		title = "Choose the output file"
	}
	return a.page(gtx, func(gtx C) D { return a.header(gtx, title, b.dir, false) }, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				ls := material.List(a.th, &b.list)
				ls.Indicator.Color = a.pal.Surface2
				return ls.Layout(gtx, len(b.entries), func(gtx C, i int) D {
					e := b.entries[i]
					ico, col := icoFile, a.pal.Text
					if e.IsDir() {
						ico, col = icoFolder, a.pal.Lavender
					} else if strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
						col = a.pal.Green
					}
					return a.listRow(&b.clicks[i], false, a.row(a.icon(ico, col, unit.Sp(14)), a.text(e.Name(), col)))(gtx)
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx C) D {
				ws := []layout.Widget{a.btn(&b.up, icoUp, "Parent", btnSecondary), a.btn(&b.hidden, icoCog, "Hidden", btnSecondary)}
				if b.saving {
					ws = append(ws, func(gtx C) D {
						gtx.Constraints.Max.X = gtx.Dp(320)
						return a.editor(&b.name, "file name", gtx.Focused(&b.name))(gtx)
					}, a.btn(&b.use, icoCheck, "Use", btnPrimary))
				}
				ws = append(ws, a.btn(&b.no, icoTimes, "Cancel", btnSecondary))
				return a.row(ws...)(gtx)
			}),
		)
	})
}
