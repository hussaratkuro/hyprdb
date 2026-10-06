// Package ui is the Gio front end of hyprdb.
package ui

import (
	"context"
	"image"
	"image/color"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/hussaratkuro/hyprdb/internal/credential"
	"github.com/hussaratkuro/hyprdb/internal/db"
	"github.com/hussaratkuro/hyprdb/internal/theme"
)

type screen int

const (
	screenHome screen = iota
	screenEdit
	screenWork
)

// A modal replaces the whole window content (an alpha dim over the ground
// would make the Hyprland window transparent).
type modal int

const (
	modalNone modal = iota
	modalUnlock
	modalPassword
	modalConfirm
	modalVaultPick
	modalInsert
	modalBrowse
	modalAddColumn
)

type App struct {
	w       *app.Window
	gtx     C // current frame context for helpers that also handle clicks
	th      *material.Theme
	pal     theme.Palette
	posted  chan func()
	pending atomic.Int32

	screen screen
	modal  modal
	status string
	isErr  bool
	busy   string // label of the latest background job
	jobs   int

	configs []db.Config
	master  string // vault password for this session

	home  homeState
	edit  editState
	work  workState
	vault vaultState
	pick  pickState
	ins   insertState
	fb    browseState
	conf  confirmState
	col   addColumnState

	backBtn, quitBtn widget.Clickable
}

type confirmState struct {
	title, text string
	ok, cancel  widget.Clickable
	action      func()
}

func Run() error {
	w := new(app.Window)
	w.Option(app.Title("hyprdb"), app.Size(unit.Dp(1280), unit.Dp(800)), app.MinSize(unit.Dp(760), unit.Dp(480)))
	a := &App{w: w, th: material.NewTheme(), posted: make(chan func(), 64)}
	a.th.Shaper = text.NewShaper(text.WithCollection(loadFonts()))
	a.th.Face = nerdFont
	a.applyTheme(theme.Current())
	theme.Watch(func(p theme.Palette) { a.post(func() { a.applyTheme(p) }) })
	a.initStates()
	cfgs, err := db.LoadConfigs()
	a.configs = cfgs
	if err != nil {
		a.setStatus(err.Error(), true)
	}

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			if a.work.conn != nil {
				a.work.conn.Close()
			}
			return e.Err
		case app.FrameEvent:
			a.drain()
			gtx := app.NewContext(&ops, e)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// post runs f on the UI goroutine. Invalidate is retried because Gio drops
// an Invalidate issued while a frame is in flight.
func (a *App) post(f func()) {
	a.posted <- f
	a.pending.Add(1)
	a.w.Invalidate()
	go func() {
		for i := 0; i < 12 && a.pending.Load() > 0; i++ {
			time.Sleep(25 * time.Millisecond)
			a.w.Invalidate()
		}
	}()
}

func (a *App) drain() {
	for {
		select {
		case f := <-a.posted:
			f()
			a.pending.Add(-1)
		default:
			return
		}
	}
}

func (a *App) applyTheme(p theme.Palette) {
	a.pal = p
	a.th.Palette = material.Palette{Bg: p.Base, Fg: p.Text, ContrastBg: p.Mauve, ContrastFg: p.OnAccent}
}

func (a *App) setStatus(msg string, isErr bool) {
	a.status, a.isErr = msg, isErr
}

// run executes job in the background with a busy label and reports its
// error (or ok message) in the status bar; done runs on the UI goroutine.
// Jobs may overlap (a job's post may start the next); the single pooled
// connection serialises them at the server.
func (a *App) run(label string, job func(ctx context.Context) (string, error), done func(err error)) {
	a.jobs++
	a.busy = label
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		ok, err := job(ctx)
		a.post(func() {
			if a.jobs--; a.jobs == 0 {
				a.busy = ""
			}
			if err != nil {
				a.setStatus(err.Error(), true)
			} else if ok != "" {
				a.setStatus(ok, false)
			}
			if done != nil {
				done(err)
			}
		})
	}()
}

// ---- frame ----

func (a *App) layout(gtx C) D {
	paint.FillShape(gtx.Ops, a.pal.Base, clip.Rect{Max: gtx.Constraints.Max}.Op())
	// Global key area goes under everything, or it blocks clicks.
	event.Op(gtx.Ops, a)
	switch a.modal {
	case modalUnlock:
		a.layoutUnlock(gtx)
	case modalPassword:
		a.layoutPassword(gtx)
	case modalConfirm:
		a.layoutConfirm(gtx)
	case modalVaultPick:
		a.layoutVaultPick(gtx)
	case modalInsert:
		a.layoutInsert(gtx)
	case modalBrowse:
		a.layoutBrowse(gtx)
	case modalAddColumn:
		a.layoutAddColumn(gtx)
	default:
		switch a.screen {
		case screenEdit:
			a.layoutEdit(gtx)
		case screenWork:
			a.layoutWork(gtx)
		default:
			a.layoutHome(gtx)
		}
	}
	a.globalKeys(gtx)
	return D{Size: gtx.Constraints.Max}
}

func (a *App) globalKeys(gtx C) {
	for {
		ev, ok := gtx.Event(key.Filter{Name: "Q", Required: key.ModCtrl}, key.Filter{Name: key.NameEscape}, key.Filter{Name: "R", Required: key.ModCtrl}, key.Filter{Name: "N", Required: key.ModCtrl})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case "Q":
			a.w.Perform(system.ActionClose)
		case "R":
			if a.screen == screenWork && a.modal == modalNone {
				a.refreshWork()
			}
		case "N":
			if a.screen == screenHome && a.modal == modalNone {
				a.openEditor(-1)
			}
		case key.NameEscape:
			a.escape()
		}
	}
}

func (a *App) escape() {
	if a.modal != modalNone {
		a.modal = modalNone
		return
	}
	switch a.screen {
	case screenEdit:
		a.screen = screenHome
	case screenWork:
		if a.work.grid.editing {
			a.work.grid.editing = false
		}
	}
}

// header is the shared top strip: back/disc, title, subtitle, trailing widgets, palette dots.
func (a *App) header(gtx C, title, sub string, back bool, trailing ...layout.Widget) D {
	return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(12), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx C) D {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				if back {
					return a.iconBtn(&a.backBtn, icoBack)(gtx)
				}
				return a.iconDisc(icoDB, a.pal.Mauve, 30)(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx C) D {
				l := material.Label(a.th, unit.Sp(17), title)
				l.Color = a.pal.Text
				l.Font.Weight = font.SemiBold
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),
			layout.Flexed(1, func(gtx C) D {
				l := material.Label(a.th, unit.Sp(12), sub)
				l.Color = a.pal.Mauve
				l.Font.Weight = font.Medium
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
		}
		for _, t := range trailing {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout), layout.Rigid(t))
		}
		children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout), layout.Rigid(a.paletteDots))
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

func (a *App) paletteDots(gtx C) D {
	cols := []color.NRGBA{a.pal.Mauve, a.pal.Yellow, a.pal.Sapphire, a.pal.Teal, a.pal.Green, a.pal.Red}
	children := make([]layout.FlexChild, 0, len(cols)*2)
	for i, c := range cols {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout))
		}
		c := c
		children = append(children, layout.Rigid(func(gtx C) D {
			d := gtx.Dp(8)
			paint.FillShape(gtx.Ops, c, clip.Ellipse{Max: image.Pt(d, d)}.Op(gtx.Ops))
			return D{Size: image.Pt(d, d)}
		}))
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

// statusBar shows the last message and the busy label.
func (a *App) statusBar(gtx C) D {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(10), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		ws := []layout.Widget{}
		if a.busy != "" {
			ws = append(ws, a.row(a.icon(icoRefresh, a.pal.Peach, unit.Sp(13)), a.small(a.busy+"…", a.pal.Peach)))
		}
		if a.status != "" {
			err, ok := "", a.status
			if a.isErr {
				err, ok = a.status, ""
			}
			ws = append(ws, a.status_(err, ok))
		}
		if len(ws) == 0 {
			ws = append(ws, a.small("ctrl+q quits · ctrl+r refreshes · esc closes dialogs", a.pal.Overlay0))
		}
		return a.row(ws...)(gtx)
	})
}

// status_ is kit's toast strip (renamed: status is the App field).
func (a *App) status_(err, ok string) layout.Widget {
	return func(gtx C) D {
		msg, c, ico := ok, a.pal.Green, icoCheck
		if err != "" {
			msg, c, ico = err, a.pal.Red, "!"
		}
		if msg == "" {
			return D{}
		}
		return a.box(gtx, alpha(c, 0x22), color.NRGBA{}, unit.Dp(8), layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}, func(gtx C) D {
			l := material.Label(a.th, unit.Sp(13), msg)
			l.Color = c
			l.MaxLines = 2
			return a.row(a.icon(ico, c, unit.Sp(14)), l.Layout)(gtx)
		})
	}
}

// page is the standard screen: header, flexible body panel, status bar.
func (a *App) page(gtx C, head layout.Widget, body layout.Widget) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(head),
		layout.Flexed(1, func(gtx C) D {
			return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min = gtx.Constraints.Max
				return a.box(gtx, alpha(a.pal.Mantle, 0xF2), a.pal.Surface0, radius, layout.UniformInset(unit.Dp(14)), func(gtx C) D {
					gtx.Constraints.Min = gtx.Constraints.Max
					return body(gtx)
				})
			})
		}),
		layout.Rigid(a.statusBar),
	)
}

// dialog centres a narrow panel; used by every modal.
func (a *App) dialog(gtx C, maxW unit.Dp, ws ...layout.Widget) D {
	return layout.Center.Layout(gtx, a.narrow(maxW, a.panel(func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return a.column(gtx, ws...)
	})))
}

// focusOnce gives the editor focus when nothing in the dialog has it.
func focusOnce(gtx C, ed *widget.Editor, others ...*widget.Editor) {
	for _, o := range others {
		if gtx.Focused(o) {
			return
		}
	}
	if !gtx.Focused(ed) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
}

// submitted reports an Enter in ed. Word keys are taken first, because
// ed.Update would otherwise consume Ctrl+Arrow before editKeys sees it.
func submitted(gtx C, ed *widget.Editor) bool {
	editKeys(gtx, ed)
	hit := false
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			return hit
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			hit = true
		}
	}
}

// ---- vault / password modals ----

type vaultState struct {
	ed         widget.Editor
	ok, no     widget.Clickable
	err        string
	busy       bool
	then       func() // continuation after unlock
	pwFor      db.Config
	pwThen     func(password string)
	pwEd       widget.Editor
	pwOK, pwNo widget.Clickable
}

// withMaster runs then once the vault password is known.
func (a *App) withMaster(then func()) {
	if a.master != "" {
		then()
		return
	}
	a.vault.then, a.vault.err = then, ""
	a.vault.ed.SetText("")
	a.modal = modalUnlock
}

func (a *App) layoutUnlock(gtx C) D {
	v := &a.vault
	if submitted(gtx, &v.ed) || v.ok.Clicked(gtx) {
		a.unlockVault()
	}
	if v.no.Clicked(gtx) {
		a.modal = modalNone
	}
	focusOnce(gtx, &v.ed)
	ws := []layout.Widget{
		a.row(a.iconDisc(icoLock, a.pal.Mauve, 34), a.heading("Unlock vault")),
		a.text("Enter the hyprvault / gopass vault password. It is kept in memory for this session only.", a.pal.Subtext0),
		a.editor(&v.ed, "Vault password", true),
		a.row(a.btn(&v.ok, icoKey, "Unlock", btnPrimary), a.btn(&v.no, icoTimes, "Cancel", btnSecondary)),
	}
	if v.err != "" {
		ws = append(ws, a.status_(v.err, ""))
	}
	if v.busy {
		ws = append(ws, a.small("checking…", a.pal.Peach))
	}
	return a.dialog(gtx, 440, ws...)
}

func (a *App) unlockVault() {
	v := &a.vault
	pw := v.ed.Text()
	if pw == "" || v.busy {
		v.err = "vault password cannot be empty"
		return
	}
	v.busy, v.err = true, ""
	go func() {
		_, err := credential.List(pw)
		a.post(func() {
			v.busy = false
			if err != nil {
				v.err = err.Error()
				v.ed.SetText("")
				return
			}
			a.master = pw
			a.modal = modalNone
			if v.then != nil {
				v.then()
			}
		})
	}()
}

// askPassword prompts for a password that is not stored anywhere.
func (a *App) askPassword(cfg db.Config, then func(password string)) {
	a.vault.pwFor, a.vault.pwThen = cfg, then
	a.vault.pwEd.SetText("")
	a.modal = modalPassword
}

func (a *App) layoutPassword(gtx C) D {
	v := &a.vault
	if submitted(gtx, &v.pwEd) || v.pwOK.Clicked(gtx) {
		pw := v.pwEd.Text()
		a.modal = modalNone
		v.pwThen(pw)
	}
	if v.pwNo.Clicked(gtx) {
		a.modal = modalNone
	}
	focusOnce(gtx, &v.pwEd)
	return a.dialog(gtx, 440,
		a.row(a.iconDisc(icoKey, a.pal.Mauve, 34), a.heading("Password")),
		a.text(v.pwFor.User+"@"+v.pwFor.Addr()+" has no vault credential; the password is used once and not saved.", a.pal.Subtext0),
		a.editor(&v.pwEd, "Password", true),
		a.row(a.btn(&v.pwOK, icoPlug, "Connect", btnPrimary), a.btn(&v.pwNo, icoTimes, "Cancel", btnSecondary)),
	)
}

func (a *App) confirm(title, text string, action func()) {
	a.conf = confirmState{title: title, text: text, action: action}
	a.modal = modalConfirm
}

func (a *App) layoutConfirm(gtx C) D {
	c := &a.conf
	if c.ok.Clicked(gtx) {
		a.modal = modalNone
		c.action()
	}
	if c.cancel.Clicked(gtx) {
		a.modal = modalNone
	}
	return a.dialog(gtx, 460,
		a.row(a.iconDisc(icoWarn, a.pal.Red, 34), a.heading(c.title)),
		a.text(c.text, a.pal.Subtext0),
		a.row(a.btn(&c.ok, icoCheck, "Yes", btnDanger), a.btn(&c.cancel, icoTimes, "Cancel", btnSecondary)),
	)
}

func (a *App) initStates() {
	a.vault.ed = newEditor(true, true, '•')
	a.vault.pwEd = newEditor(true, true, '•')
	a.initHome()
	a.initEdit()
	a.initWork()
}
