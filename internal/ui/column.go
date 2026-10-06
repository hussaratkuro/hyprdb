package ui

import (
	"context"
	"image/color"
	"strings"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/hussaratkuro/hyprdb/internal/db"
)

// combo is an editable text field with a drop-down of suggestions.
type combo struct {
	ed     widget.Editor
	btn    widget.Clickable
	open   bool
	opts   []string
	clicks []widget.Clickable
}

func newCombo(opts []string, value string) combo {
	c := combo{ed: newEditor(true, true, 0), opts: opts, clicks: make([]widget.Clickable, len(opts))}
	c.ed.SetText(value)
	return c
}

func (a *App) layoutCombo(gtx C, label string, c *combo, hint string) D {
	if c.btn.Clicked(gtx) {
		c.open = !c.open
	}
	for i := range c.opts {
		if c.clicks[i].Clicked(gtx) {
			c.ed.SetText(c.opts[i])
			c.ed.SetCaret(len([]rune(c.opts[i])), len([]rune(c.opts[i])))
			c.open = false
		}
	}
	ico := icoDown
	if c.open {
		ico = icoUp
	}
	ws := []layout.Widget{
		func(gtx C) D {
			return layout.Flex{Alignment: layout.End}.Layout(gtx,
				layout.Flexed(1, a.field(label, &c.ed, hint, gtx.Focused(&c.ed))),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(a.btn(&c.btn, ico, "", btnSecondary)),
			)
		},
	}
	if c.open {
		ws = append(ws, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return a.box(gtx, a.pal.Surface0, a.pal.Surface1, unit.Dp(8), layout.UniformInset(unit.Dp(4)), func(gtx C) D {
				rows := make([]layout.FlexChild, len(c.opts))
				for i, o := range c.opts {
					i, o := i, o
					rows[i] = layout.Rigid(func(gtx C) D {
						return c.clicks[i].Layout(gtx, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							bg := color.NRGBA{}
							if c.clicks[i].Hovered() {
								bg = a.pal.Surface1
							}
							return a.box(gtx, bg, color.NRGBA{}, unit.Dp(6), layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}, func(gtx C) D {
								pointer.CursorPointer.Add(gtx.Ops)
								return a.mono(o, a.pal.Text, unit.Sp(13))(gtx)
							})
						})
					})
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Rigid(ws[0]), layout.Rigid(func(gtx C) D {
		if len(ws) < 2 {
			return D{}
		}
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, ws[1])
	}))
}

// ---- add column dialog ----

type addColumnState struct {
	name                        widget.Editor
	typ, null, def, after, comm combo
	list                        widget.List
	ok, no                      widget.Clickable
}

var (
	mysqlTypes = []string{"INT", "INT UNSIGNED", "BIGINT", "TINYINT(1)", "SMALLINT", "DECIMAL(10,2)", "FLOAT", "DOUBLE", "VARCHAR(255)", "CHAR(36)", "TEXT", "MEDIUMTEXT", "LONGTEXT", "DATE", "DATETIME", "TIMESTAMP", "TIME", "YEAR", "JSON", "BLOB", "ENUM('a','b')"}
	pgTypes    = []string{"integer", "bigint", "smallint", "serial", "bigserial", "numeric(10,2)", "real", "double precision", "boolean", "varchar(255)", "text", "uuid", "date", "timestamp", "timestamptz", "time", "interval", "jsonb", "bytea"}
	defaults   = []string{"NULL", "0", "''", "CURRENT_TIMESTAMP", "true", "false"}
)

func (a *App) openAddColumn() {
	w := &a.work
	s := &a.col
	types := mysqlTypes
	if !w.conn.IsMySQL() {
		types = pgTypes
	}
	s.name = newEditor(true, true, 0)
	s.typ = newCombo(types, types[0])
	s.null = newCombo([]string{"NULL", "NOT NULL"}, "NULL")
	s.def = newCombo(defaults, "")
	s.comm = newCombo(nil, "")
	names := []string{"FIRST"}
	for _, c := range w.cols {
		names = append(names, c.Name)
	}
	last := ""
	if len(w.cols) > 0 {
		last = w.cols[len(w.cols)-1].Name
	}
	s.after = newCombo(names, last)
	s.list.Axis = layout.Vertical
	a.modal = modalAddColumn
}

func (a *App) layoutAddColumn(gtx C) D {
	w := &a.work
	s := &a.col
	eds := []*widget.Editor{&s.name, &s.typ.ed, &s.null.ed, &s.def.ed, &s.after.ed, &s.comm.ed}
	submit := s.ok.Clicked(gtx)
	for _, ed := range eds {
		submit = submitted(gtx, ed) || submit
	}
	if s.no.Clicked(gtx) {
		a.modal = modalNone
	}
	if submit {
		a.addColumn()
	}
	focusOnce(gtx, &s.name, eds[1:]...)
	mysql := w.conn.IsMySQL()
	return layout.Center.Layout(gtx, a.narrow(620, a.panel(func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(680))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.row(a.iconDisc(icoColumns, a.pal.Mauve, 34), a.heading("Add column to "+w.table))),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(a.small("every drop-down is also a free text field; the statement is ALTER TABLE … ADD COLUMN", a.pal.Subtext0)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Flexed(1, func(gtx C) D {
				ls := material.List(a.th, &s.list)
				ls.Indicator.Color = a.pal.Surface2
				ws := []layout.Widget{
					a.field("Name", &s.name, "new_column", gtx.Focused(&s.name)),
					func(gtx C) D { return a.layoutCombo(gtx, "Type", &s.typ, "") },
					func(gtx C) D { return a.layoutCombo(gtx, "Nullability", &s.null, "") },
					func(gtx C) D { return a.layoutCombo(gtx, "Default (empty = none)", &s.def, "") },
				}
				if mysql {
					ws = append(ws,
						func(gtx C) D { return a.layoutCombo(gtx, "Position: AFTER column (or FIRST)", &s.after, "") },
						a.field("Comment", &s.comm.ed, "", gtx.Focused(&s.comm.ed)))
				} else {
					ws = append(ws, a.small("PostgreSQL always appends new columns at the end.", a.pal.Overlay1))
				}
				return ls.Layout(gtx, len(ws), func(gtx C, i int) D {
					return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, ws[i])
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
			layout.Rigid(a.row(a.btn(&s.ok, icoCheck, "Add column", btnPrimary), a.btn(&s.no, icoTimes, "Cancel", btnSecondary))),
		)
	})))
}

func (a *App) addColumn() {
	w := &a.work
	s := &a.col
	col := db.Column{Name: strings.TrimSpace(s.name.Text()), Type: strings.TrimSpace(s.typ.ed.Text()), Default: strings.TrimSpace(s.def.ed.Text()), Comment: strings.TrimSpace(s.comm.ed.Text())}
	col.Nullable = !strings.EqualFold(strings.TrimSpace(s.null.ed.Text()), "NOT NULL")
	if col.Name == "" || col.Type == "" {
		a.setStatus("column name and type are required", true)
		return
	}
	after := strings.TrimSpace(s.after.ed.Text())
	conn, schema, table := w.conn, w.schema, w.table
	a.modal = modalNone
	a.run("adding column "+col.Name, func(ctx context.Context) (string, error) {
		if err := conn.AddColumn(ctx, schema, table, col, after); err != nil {
			return "", err
		}
		a.post(a.refreshWork)
		return "column " + col.Name + " added to " + table, nil
	}, nil)
}
