package ui

import (
	"context"
	"fmt"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type queryState struct {
	ed   widget.Editor
	run  widget.Clickable
	grid grid
	note string
}

func (a *App) initQuery() {
	q := &a.work.query
	q.ed = widget.Editor{}
	q.grid = newGrid()
}

func (a *App) layoutQuery(gtx C) D {
	q := &a.work.query
	// Ctrl+Enter runs; it must be taken from the editor before ed.Update.
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &q.ed, Name: key.NameReturn, Required: key.ModCtrl})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			a.runQuery()
		}
	}
	if q.run.Clicked(gtx) {
		a.runQuery()
	}
	editKeys(gtx, &q.ed)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.Y = gtx.Dp(140)
			gtx.Constraints.Max.Y = gtx.Dp(220)
			return a.box(gtx, a.pal.Surface0, a.pal.Surface1, unit.Dp(8), layout.UniformInset(unit.Dp(10)), func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				e := material.Editor(a.th, &q.ed, "SELECT … (ctrl+enter runs; selected text runs alone)")
				e.Color, e.HintColor, e.TextSize = a.pal.Text, a.pal.Overlay0, unit.Sp(13)
				e.SelectionColor = alpha(a.pal.Mauve, 0x66)
				return e.Layout(gtx)
			})
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Rigid(a.row(a.btn(&q.run, icoPlay, "Run", btnPrimary), a.small(q.note, a.pal.Subtext0))),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx C) D { return a.layoutGrid(gtx, &q.grid) }),
	)
}

func (a *App) runQuery() {
	w := &a.work
	q := &w.query
	sqlText := q.ed.SelectedText()
	if strings.TrimSpace(sqlText) == "" {
		sqlText = q.ed.Text()
	}
	if strings.TrimSpace(sqlText) == "" {
		return
	}
	conn := w.conn
	a.run("running query", func(ctx context.Context) (string, error) {
		a.selectSchema(ctx) // unqualified names resolve in the selected schema
		res, err := conn.Query(ctx, sqlText)
		if err != nil {
			return "", err
		}
		a.post(func() {
			q.grid.set(res)
			if res.IsQuery {
				q.note = fmt.Sprintf("%d rows · %s", len(res.Rows), res.Elapsed.Round(1e5))
			} else {
				q.note = fmt.Sprintf("%d rows affected · %s", res.Affected, res.Elapsed.Round(1e5))
				a.openSchema(w.open)
			}
		})
		return "", nil
	}, nil)
}
