package ui

import (
	"context"
	"fmt"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/hussaratkuro/hyprdb/internal/db"
)

const pageSize = 200

type dataState struct {
	where                                     widget.Editor
	apply, prev, next, add, addCol, del, null widget.Clickable
	page                                      int
	total                                     int64
}

type insertState struct {
	eds    []widget.Editor
	list   widget.List
	ok, no widget.Clickable
}

func (a *App) initData() {
	a.work.data.where = newEditor(true, true, 0)
	a.ins.list.Axis = layout.Vertical
	a.work.grid.onEdit = a.commitCell
}

func (a *App) loadPage() {
	w := &a.work
	conn, schema, table, pk := w.conn, w.schema, w.table, w.pk
	where, page := w.data.where.Text(), w.data.page
	a.run("loading rows", func(ctx context.Context) (string, error) {
		res, total, err := conn.Page(ctx, schema, table, pk, where, page*pageSize, pageSize)
		if err != nil {
			return "", err
		}
		a.post(func() {
			if w.schema == schema && w.table == table {
				w.grid.set(res)
				w.data.total = total
			}
		})
		return "", nil
	}, nil)
}

func (a *App) commitCell(row, col int, v db.Value) {
	w := &a.work
	conn, schema, table, pk := w.conn, w.schema, w.table, w.pk
	res := w.grid.res
	old := res.Rows[row][col]
	if old == v {
		return
	}
	colName := res.Columns[col]
	snapshot := append([]db.Value(nil), res.Rows[row]...)
	a.run("updating "+colName, func(ctx context.Context) (string, error) {
		if err := conn.UpdateCell(ctx, schema, table, pk, res, snapshot, colName, v); err != nil {
			return "", err
		}
		a.post(func() {
			if w.grid.res == res {
				res.Rows[row][col] = v
			}
		})
		return "updated " + colName, nil
	}, nil)
}

func (a *App) layoutData(gtx C) D {
	w := &a.work
	d := &w.data
	g := &w.grid
	if w.cols != nil && len(w.pk) == 0 {
		g.onEdit = nil
	} else {
		g.onEdit = a.commitCell
	}
	if submitted(gtx, &d.where) || d.apply.Clicked(gtx) {
		d.page = 0
		a.loadPage()
	}
	if d.prev.Clicked(gtx) && d.page > 0 {
		d.page--
		a.loadPage()
	}
	if d.next.Clicked(gtx) && int64((d.page+1)*pageSize) < d.total {
		d.page++
		a.loadPage()
	}
	if d.add.Clicked(gtx) && w.cols != nil {
		a.openInsert()
	}
	if d.addCol.Clicked(gtx) && w.cols != nil && w.info.Type == "TABLE" {
		a.openAddColumn()
	}
	if d.null.Clicked(gtx) && g.onEdit != nil {
		if _, ok := g.selected(); ok {
			g.editing = false
			a.commitCell(g.selRow, g.selCol, db.Value{Null: true})
		}
	}
	if d.del.Clicked(gtx) && g.onEdit != nil {
		if row, ok := g.selected(); ok {
			a.deleteRow(row)
		}
	}
	from, to := d.page*pageSize+1, min(int64((d.page+1)*pageSize), d.total)
	if d.total == 0 {
		from = 0
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, a.editor(&d.where, "WHERE … (press Enter)", gtx.Focused(&d.where))),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(a.row(a.btn(&d.apply, icoSearch, "", btnSecondary), a.btn(&d.prev, icoLeft, "", btnSecondary),
					a.mono(fmt.Sprintf("%d–%d of %d", from, to, d.total), a.pal.Subtext1, unit.Sp(12)), a.btn(&d.next, icoRight, "", btnSecondary),
					a.btn(&d.add, icoPlus, "Row", btnSecondary), a.btn(&d.addCol, icoPlus, "Column", btnSecondary), a.btn(&d.null, "", "NULL", btnSecondary), a.btn(&d.del, icoDelete, "", btnDanger))),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx C) D { return a.layoutGrid(gtx, g) }),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(a.small("double-click a cell to edit · Enter saves · Esc cancels · NULL sets the selected cell to NULL", a.pal.Overlay0)),
	)
}

func (a *App) deleteRow(row []db.Value) {
	w := &a.work
	conn, schema, table, pk, res := w.conn, w.schema, w.table, w.pk, w.grid.res
	keys := make([]string, len(pk))
	for i, k := range pk {
		keys[i] = k + "=" + res.Get(row, k)
	}
	a.confirm("Delete row", "DELETE FROM "+table+" WHERE "+strings.Join(keys, " AND ")+" ?", func() {
		a.run("deleting row", func(ctx context.Context) (string, error) {
			if err := conn.DeleteRow(ctx, schema, table, pk, res, row); err != nil {
				return "", err
			}
			a.post(a.loadPage)
			return "row deleted", nil
		}, nil)
	})
}

// ---- insert dialog ----

func (a *App) openInsert() {
	w := &a.work
	a.ins.eds = make([]widget.Editor, len(w.cols))
	for i := range a.ins.eds {
		a.ins.eds[i] = newEditor(true, true, 0)
	}
	a.modal = modalInsert
}

func (a *App) layoutInsert(gtx C) D {
	w := &a.work
	in := &a.ins
	submit := in.ok.Clicked(gtx)
	for i := range in.eds {
		submit = submitted(gtx, &in.eds[i]) || submit
	}
	if in.no.Clicked(gtx) {
		a.modal = modalNone
	}
	if submit {
		var cols []string
		var vals []db.Value
		for i, c := range w.cols {
			t := in.eds[i].Text()
			if t == "" {
				continue
			}
			if t == "NULL" {
				vals = append(vals, db.Value{Null: true})
			} else {
				vals = append(vals, db.Value{Text: t})
			}
			cols = append(cols, c.Name)
		}
		conn, schema, table := w.conn, w.schema, w.table
		a.modal = modalNone
		a.run("inserting", func(ctx context.Context) (string, error) {
			if err := conn.InsertRow(ctx, schema, table, cols, vals); err != nil {
				return "", err
			}
			a.post(a.loadPage)
			return "row inserted", nil
		}, nil)
	}
	if len(in.eds) > 0 {
		others := make([]*widget.Editor, 0, len(in.eds))
		for i := range in.eds[1:] {
			others = append(others, &in.eds[i+1])
		}
		focusOnce(gtx, &in.eds[0], others...)
	}
	return layout.Center.Layout(gtx, a.narrow(640, a.panel(func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(640))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.row(a.iconDisc(icoPlus, a.pal.Mauve, 34), a.heading("Insert into "+w.table))),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(a.small("empty fields are omitted (defaults apply); type NULL for an explicit NULL", a.pal.Subtext0)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Flexed(1, func(gtx C) D {
				ls := material.List(a.th, &in.list)
				ls.Indicator.Color = a.pal.Surface2
				return ls.Layout(gtx, len(w.cols), func(gtx C, i int) D {
					c := w.cols[i]
					hint := c.Type
					if c.Default != "" {
						hint += " · default " + c.Default
					}
					if c.Extra != "" {
						hint += " · " + c.Extra
					}
					return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, a.field(c.Name, &in.eds[i], hint, gtx.Focused(&in.eds[i])))
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(a.row(a.btn(&in.ok, icoCheck, "Insert", btnPrimary), a.btn(&in.no, icoTimes, "Cancel", btnSecondary))),
		)
	})))
}
