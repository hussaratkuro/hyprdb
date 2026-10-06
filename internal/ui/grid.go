package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/hussaratkuro/hyprdb/internal/db"
)

// grid renders a db.Result as a scrollable table with an optional editable
// cell. Horizontal scrolling wraps a vertical list (Shift+wheel scrolls X).
type grid struct {
	res     *db.Result
	widths  []int
	vlist   widget.List
	hlist   widget.List
	clicks  []gesture.Click
	selRow  int
	selCol  int
	editing bool
	ed      widget.Editor
	// onEdit commits an edited cell; nil = read only.
	onEdit func(row, col int, v db.Value)
}

func newGrid() grid {
	g := grid{selRow: -1, selCol: -1}
	g.vlist.Axis, g.hlist.Axis = layout.Vertical, layout.Horizontal
	g.ed = widget.Editor{Submit: true} // multi-line: long values wrap in the overlay
	return g
}

func (g *grid) set(res *db.Result) {
	g.res, g.widths, g.editing = res, nil, false
	g.selRow, g.selCol = -1, -1
	g.vlist.Position = layout.Position{}
	if res != nil && len(g.clicks) < len(res.Rows) {
		g.clicks = make([]gesture.Click, len(res.Rows))
	}
}

func (g *grid) selected() ([]db.Value, bool) {
	if g.res == nil || g.selRow < 0 || g.selRow >= len(g.res.Rows) {
		return nil, false
	}
	return g.res.Rows[g.selRow], true
}

func (g *grid) startEdit() {
	if g.onEdit == nil || g.selRow < 0 {
		return
	}
	v := g.res.Rows[g.selRow][g.selCol]
	g.ed.SetText(v.Text)
	g.editing = true
}

func (a *App) measure(gtx C, g *grid) {
	if g.widths != nil || g.res == nil {
		return
	}
	cw := float32(gtx.Sp(13)) * 0.62
	g.widths = make([]int, len(g.res.Columns))
	for i, c := range g.res.Columns {
		n := len(c) + 2
		for r, row := range g.res.Rows {
			if r > 300 {
				break
			}
			if l := len(row[i].Text); l > n {
				n = l
			}
		}
		g.widths[i] = min(max(int(cw*float32(n))+gtx.Dp(18), gtx.Dp(70)), gtx.Dp(420))
	}
}

func (a *App) layoutGrid(gtx C, g *grid) D {
	if g.res == nil || len(g.res.Columns) == 0 {
		msg := "no result"
		if g.res != nil && !g.res.IsQuery {
			msg = "statement ok"
		}
		return layout.Center.Layout(gtx, a.small(msg, a.pal.Overlay0))
	}
	a.measure(gtx, g)
	// Editing: Escape cancels, Return commits.
	if g.editing {
		editKeys(gtx, &g.ed)
		for {
			ev, ok := gtx.Event(key.Filter{Focus: &g.ed, Name: key.NameEscape})
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press {
				g.editing = false
			}
		}
		if submitted(gtx, &g.ed) {
			g.editing = false
			g.onEdit(g.selRow, g.selCol, db.Value{Text: g.ed.Text()})
		}
		if g.editing && !gtx.Focused(&g.ed) {
			gtx.Execute(key.FocusCmd{Tag: &g.ed})
		}
	}
	total := 0
	for _, w := range g.widths {
		total += w
	}
	rowH := gtx.Dp(26)
	ls := material.List(a.th, &g.hlist)
	ls.Indicator.Color = a.pal.Surface2
	return ls.Layout(gtx, 1, func(gtx C, _ int) D {
		gtx.Constraints = layout.Exact(image.Pt(max(total, gtx.Constraints.Min.X), gtx.Constraints.Max.Y))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return a.gridRow(gtx, g, -1, rowH) }),
			layout.Flexed(1, func(gtx C) D {
				vl := material.List(a.th, &g.vlist)
				vl.Indicator.Color = a.pal.Surface2
				return vl.Layout(gtx, len(g.res.Rows), func(gtx C, i int) D {
					return a.gridRow(gtx, g, i, rowH)
				})
			}),
		)
	})
}

// gridRow draws the header (i == -1) or data row i.
func (a *App) gridRow(gtx C, g *grid, i int, rowH int) D {
	total := 0
	for _, w := range g.widths {
		total += w
	}
	size := image.Pt(total, rowH)
	bg := color.NRGBA{}
	switch {
	case i < 0:
		bg = a.pal.Surface0
	case i == g.selRow:
		bg = alpha(a.pal.Mauve, 0x18)
	case i%2 == 1:
		bg = alpha(a.pal.Surface0, 0x50)
	}
	if bg.A != 0 {
		paint.FillShape(gtx.Ops, bg, clip.Rect{Max: size}.Op())
	}
	if i >= 0 {
		// Click handling: single selects, double edits.
		for {
			ev, ok := g.clicks[i].Update(gtx.Source)
			if !ok {
				break
			}
			if ev.Kind != gesture.KindClick {
				continue
			}
			col := g.colAt(ev.Position.X)
			if g.editing && (g.selRow != i || g.selCol != col) {
				g.editing = false
			}
			g.selRow, g.selCol = i, col
			if ev.NumClicks >= 2 {
				g.startEdit()
			}
		}
		defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
		g.clicks[i].Add(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
	}
	x := 0
	for c, w := range g.widths {
		cell := image.Rect(x, 0, x+w, rowH)
		st := op.Offset(cell.Min).Push(gtx.Ops)
		cl := clip.Rect{Max: cell.Size()}.Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(cell.Size())
		a.cellLabel(cg, g, i, c)
		if i >= 0 && g.editing && i == g.selRow && c == g.selCol {
			// The editor overlays the cell and may grow to the right and
			// downwards; Defer resets the row/list clips so it is not cut off.
			og := gtx
			longest := 0
			for _, line := range strings.Split(g.ed.Text(), "\n") {
				longest = max(longest, len([]rune(line)))
			}
			want := int(float32(gtx.Sp(13))*0.62*float32(longest+2)) + gtx.Dp(28)
			maxW := max(total-x, gtx.Dp(360))
			og.Constraints = layout.Constraints{Min: image.Pt(w, rowH), Max: image.Pt(min(max(want, w), maxW), gtx.Dp(200))}
			m := op.Record(gtx.Ops)
			a.cellEditor(og, g)
			op.Defer(gtx.Ops, m.Stop())
		}
		if i >= 0 && i == g.selRow && c == g.selCol {
			paint.FillShape(gtx.Ops, a.pal.Mauve, clip.Stroke{Path: clip.Rect{Max: cell.Size()}.Path(), Width: float32(gtx.Dp(1))}.Op())
		}
		cl.Pop()
		st.Pop()
		// Column separator.
		paint.FillShape(gtx.Ops, alpha(a.pal.Surface1, 0x80), clip.Rect{Min: image.Pt(x+w-1, 0), Max: image.Pt(x+w, rowH)}.Op())
		x += w
	}
	return D{Size: size}
}

func (a *App) cellLabel(gtx C, g *grid, i, c int) {
	txt, col, weight := g.res.Columns[c], a.pal.Subtext1, font.SemiBold
	if i >= 0 {
		v := g.res.Rows[i][c]
		txt, col, weight = v.Text, a.pal.Text, font.Normal
		if v.Null {
			txt, col = "NULL", a.pal.Overlay0
		}
		if nl := strings.IndexByte(txt, '\n'); nl >= 0 {
			txt = txt[:nl] + " ↵"
		}
	}
	layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D {
			l := material.Label(a.th, unit.Sp(13), txt)
			l.Color, l.Font.Weight, l.MaxLines = col, weight, 1
			if i >= 0 && g.res.Rows[i][c].Null {
				l.Font.Style = font.Italic
			}
			return l.Layout(gtx)
		})
	})
}

func (a *App) cellEditor(gtx C, g *grid) D {
	return a.box(gtx, a.pal.Surface0, a.pal.Mauve, unit.Dp(4), layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		e := material.Editor(a.th, &g.ed, "")
		e.Color, e.TextSize = a.pal.Text, unit.Sp(13)
		e.SelectionColor = alpha(a.pal.Mauve, 0x66)
		return e.Layout(gtx)
	})
}

func (g *grid) colAt(x int) int {
	for c, w := range g.widths {
		if x < w {
			return c
		}
		x -= w
	}
	return max(len(g.widths)-1, 0)
}
