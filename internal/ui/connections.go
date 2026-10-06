package ui

import (
	"context"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/hussaratkuro/hyprdb/internal/credential"
	"github.com/hussaratkuro/hyprdb/internal/db"
)

// ---- home: saved connections ----

type homeState struct {
	list                    widget.List
	open, edit, del, newBtn []widget.Clickable
	add                     widget.Clickable
}

func (a *App) initHome() { a.home.list.Axis = layout.Vertical }

func (a *App) layoutHome(gtx C) D {
	h := &a.home
	for len(h.open) < len(a.configs) {
		h.open, h.edit, h.del = append(h.open, widget.Clickable{}), append(h.edit, widget.Clickable{}), append(h.del, widget.Clickable{})
	}
	if h.add.Clicked(gtx) {
		a.openEditor(-1)
	}
	for i := range a.configs {
		if h.open[i].Clicked(gtx) {
			a.connect(a.configs[i])
		}
		if h.edit[i].Clicked(gtx) {
			a.openEditor(i)
		}
		if h.del[i].Clicked(gtx) {
			cfg := a.configs[i]
			a.confirm("Delete connection", "Remove \""+cfg.Name+"\" from the saved connections?", func() {
				a.configs = append(a.configs[:i:i], a.configs[i+1:]...)
				a.saveConfigs()
			})
		}
	}
	return a.page(gtx, func(gtx C) D {
		return a.header(gtx, "hyprdb", "MySQL · MariaDB · PostgreSQL", false, a.btn(&h.add, icoPlus, "New connection", btnPrimary))
	}, func(gtx C) D {
		if len(a.configs) == 0 {
			return layout.Center.Layout(gtx, func(gtx C) D {
				return a.column(gtx, a.heading("No saved connections"), a.text("Create one with \"New connection\". Passwords come from hyprvault / gopass or are asked at connect time.", a.pal.Subtext0))
			})
		}
		ls := material.List(a.th, &h.list)
		ls.Indicator.Color = a.pal.Surface2
		return ls.Layout(gtx, len(a.configs), func(gtx C, i int) D {
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, a.connCard(i))
		})
	})
}

func (a *App) connCard(i int) layout.Widget {
	cfg, h := a.configs[i], &a.home
	return func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		border := a.pal.Surface1
		if h.open[i].Hovered() {
			border = a.pal.Mauve
		}
		return a.box(gtx, a.pal.Surface0, border, unit.Dp(10), layout.UniformInset(unit.Dp(12)), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return h.open[i].Layout(gtx, func(gtx C) D {
						pointer.CursorPointer.Add(gtx.Ops)
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(a.iconDisc(icoDB, a.kindColor(cfg.Kind), 36)),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							layout.Flexed(1, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										l := material.Label(a.th, unit.Sp(15), cfg.Name)
										l.Color, l.Font.Weight, l.MaxLines = a.pal.Text, font.SemiBold, 1
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
									layout.Rigid(a.row(a.pill(cfg.Kind.Label(), a.kindColor(cfg.Kind)), a.small(cfg.User+"@"+cfg.Addr()+"/"+cfg.Database, a.pal.Subtext0), a.credBadge(cfg))),
								)
							}),
						)
					})
				}),
				layout.Rigid(a.row(a.iconBtn(&h.edit[i], icoEdit), a.iconBtn(&h.del[i], icoDelete))),
			)
		})
	}
}

func (a *App) credBadge(cfg db.Config) layout.Widget {
	if cfg.CredentialRef == "" {
		return a.small("· password asked", a.pal.Overlay0)
	}
	return a.row(a.icon(icoLock, a.pal.Green, unit.Sp(11)), a.small(cfg.CredentialRef, a.pal.Green))
}

func (a *App) kindColor(k db.Kind) color.NRGBA {
	switch k {
	case db.MariaDB:
		return a.pal.Peach
	case db.Postgres:
		return a.pal.Sapphire
	}
	return a.pal.Teal
}

func (a *App) pill(s string, c color.NRGBA) layout.Widget {
	return func(gtx C) D {
		return a.box(gtx, alpha(c, 0x30), color.NRGBA{}, unit.Dp(6), layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(7), Right: unit.Dp(7)}, a.small(s, c))
	}
}

func (a *App) saveConfigs() {
	if err := db.SaveConfigs(a.configs); err != nil {
		a.setStatus(err.Error(), true)
		return
	}
	a.setStatus("saved "+db.ConfigPath(), false)
}

// connect resolves the password (vault or prompt) and opens the workspace.
func (a *App) connect(cfg db.Config) {
	open := func(password string) {
		a.run("connecting to "+cfg.Name, func(ctx context.Context) (string, error) {
			conn, err := db.Open(cfg, password)
			if err != nil {
				return "", err
			}
			a.post(func() { a.enterWork(conn) })
			return "connected to " + cfg.Name, nil
		}, nil)
	}
	if cfg.CredentialRef == "" {
		a.askPassword(cfg, open)
		return
	}
	a.withMaster(func() {
		a.run("resolving credential "+cfg.CredentialRef, func(ctx context.Context) (string, error) {
			c, err := credential.Get(cfg.CredentialRef, a.master)
			if err != nil {
				return "", err
			}
			if cfg.User == "" {
				cfg.User = c.Username
			}
			a.post(func() { open(c.Password) })
			return "", nil
		}, nil)
	})
}

// ---- editor ----

type editState struct {
	idx                                 int
	name, host, port, user, dbname, ref widget.Editor
	kind                                db.Kind
	kindBtns                            [3]widget.Clickable
	save, cancel, test, pickRef         widget.Clickable
}

func (a *App) initEdit() {
	e := &a.edit
	for _, ed := range []*widget.Editor{&e.name, &e.host, &e.port, &e.user, &e.dbname, &e.ref} {
		*ed = newEditor(true, true, 0)
	}
}

func (a *App) openEditor(idx int) {
	e := &a.edit
	cfg := db.Config{Kind: db.MariaDB, Host: "localhost", Port: 3306}
	if idx >= 0 {
		cfg = a.configs[idx]
	}
	e.idx, e.kind = idx, cfg.Kind
	e.name.SetText(cfg.Name)
	e.host.SetText(cfg.Host)
	e.port.SetText(strconv.Itoa(cfg.Port))
	e.user.SetText(cfg.User)
	e.dbname.SetText(cfg.Database)
	e.ref.SetText(cfg.CredentialRef)
	a.status = ""
	a.screen = screenEdit
}

func (a *App) editorConfig() (db.Config, error) {
	e := &a.edit
	port, _ := strconv.Atoi(strings.TrimSpace(e.port.Text()))
	cfg := db.Config{Kind: e.kind, Name: strings.TrimSpace(e.name.Text()), Host: strings.TrimSpace(e.host.Text()), Port: port,
		User: strings.TrimSpace(e.user.Text()), Database: strings.TrimSpace(e.dbname.Text()), CredentialRef: strings.TrimSpace(e.ref.Text())}
	if e.idx >= 0 {
		cfg.ID = a.configs[e.idx].ID
	}
	return cfg, cfg.Validate()
}

func (a *App) layoutEdit(gtx C) D {
	e := &a.edit
	eds := []*widget.Editor{&e.name, &e.host, &e.port, &e.user, &e.dbname, &e.ref}
	for i, k := range db.Kinds {
		if e.kindBtns[i].Clicked(gtx) {
			if p, _ := strconv.Atoi(e.port.Text()); p == e.kind.DefaultPort() {
				e.port.SetText(strconv.Itoa(k.DefaultPort()))
			}
			e.kind = k
		}
	}
	save := e.save.Clicked(gtx)
	for _, ed := range eds {
		save = submitted(gtx, ed) || save
	}
	if save {
		cfg, err := a.editorConfig()
		if err != nil {
			a.setStatus(err.Error(), true)
		} else {
			if e.idx >= 0 {
				a.configs[e.idx] = cfg
			} else {
				a.configs = append(a.configs, cfg)
			}
			a.saveConfigs()
			a.screen = screenHome
		}
	}
	if e.cancel.Clicked(gtx) || a.backBtn.Clicked(gtx) {
		a.screen = screenHome
	}
	if e.test.Clicked(gtx) {
		if cfg, err := a.editorConfig(); err != nil {
			a.setStatus(err.Error(), true)
		} else {
			a.testConnection(cfg)
		}
	}
	if e.pickRef.Clicked(gtx) {
		a.withMaster(a.openVaultPick)
	}
	focusOnce(gtx, &e.name, eds[1:]...)
	title := "New connection"
	if e.idx >= 0 {
		title = "Edit connection"
	}
	return a.page(gtx, func(gtx C) D { return a.header(gtx, title, "", true) }, func(gtx C) D {
		return layout.Center.Layout(gtx, a.narrow(640, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			kinds := make([]layout.Widget, len(db.Kinds))
			for i, k := range db.Kinds {
				i, k := i, k
				kinds[i] = a.choice(&e.kindBtns[i], k.Label(), e.kind == k)
			}
			return a.column(gtx,
				a.row(kinds...),
				a.field("Name", &e.name, "production db", gtx.Focused(&e.name)),
				a.row(func(gtx C) D {
					gtx.Constraints.Max.X = gtx.Constraints.Max.X * 7 / 10
					return a.field("Host", &e.host, "localhost", gtx.Focused(&e.host))(gtx)
				}, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return a.field("Port", &e.port, "3306", gtx.Focused(&e.port))(gtx)
				}),
				a.field("User", &e.user, "root", gtx.Focused(&e.user)),
				a.field("Database (required for PostgreSQL, default schema for MySQL)", &e.dbname, "", gtx.Focused(&e.dbname)),
				func(gtx C) D {
					return layout.Flex{Alignment: layout.End}.Layout(gtx,
						layout.Flexed(1, a.field("hyprvault / gopass entry (ID or title; empty = ask at connect)", &e.ref, "", gtx.Focused(&e.ref))),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(a.btn(&e.pickRef, icoLock, "Pick", btnSecondary)),
					)
				},
				a.row(a.btn(&e.save, icoCheck, "Save", btnPrimary), a.btn(&e.test, icoPlug, "Test", btnSecondary), a.btn(&e.cancel, icoTimes, "Cancel", btnSecondary)),
			)
		}))
	})
}

func (a *App) choice(c *widget.Clickable, label string, on bool) layout.Widget {
	return func(gtx C) D {
		bg, fg := a.pal.Surface0, a.pal.Subtext1
		if on {
			bg, fg = alpha(a.pal.Mauve, 0x40), a.pal.Text
		} else if c.Hovered() {
			bg = a.pal.Surface1
		}
		return c.Layout(gtx, func(gtx C) D {
			return a.box(gtx, bg, color.NRGBA{}, unit.Dp(8), layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				return a.mono(label, fg, unit.Sp(13))(gtx)
			})
		})
	}
}

func (a *App) testConnection(cfg db.Config) {
	try := func(password string) {
		a.run("testing "+cfg.Addr(), func(ctx context.Context) (string, error) {
			conn, err := db.Open(cfg, password)
			if err != nil {
				return "", err
			}
			defer conn.Close()
			return "connection ok: " + conn.Version(), nil
		}, nil)
	}
	if cfg.CredentialRef == "" {
		a.askPassword(cfg, try)
		return
	}
	a.withMaster(func() {
		a.run("resolving credential", func(ctx context.Context) (string, error) {
			c, err := credential.Get(cfg.CredentialRef, a.master)
			if err != nil {
				return "", err
			}
			a.post(func() { try(c.Password) })
			return "", nil
		}, nil)
	})
}

// ---- vault entry picker ----

type pickState struct {
	entries []credential.Credential
	clicks  []widget.Clickable
	list    widget.List
	filter  widget.Editor
	cancel  widget.Clickable
}

func (a *App) openVaultPick() {
	p := &a.pick
	p.list.Axis = layout.Vertical
	p.filter = newEditor(true, false, 0)
	a.run("listing vault", func(ctx context.Context) (string, error) {
		l, err := credential.List(a.master)
		if err != nil {
			return "", err
		}
		a.post(func() {
			p.entries, p.clicks = l, make([]widget.Clickable, len(l))
			a.modal = modalVaultPick
		})
		return "", nil
	}, nil)
}

func (a *App) layoutVaultPick(gtx C) D {
	p := &a.pick
	if p.cancel.Clicked(gtx) {
		a.modal = modalNone
	}
	q := strings.ToLower(strings.TrimSpace(p.filter.Text()))
	var vis []int
	for i, e := range p.entries {
		if q == "" || strings.Contains(strings.ToLower(e.Title+" "+e.Username+" "+e.URL), q) {
			vis = append(vis, i)
		}
	}
	for _, i := range vis {
		if p.clicks[i].Clicked(gtx) {
			a.edit.ref.SetText(p.entries[i].Title)
			if a.edit.user.Text() == "" {
				a.edit.user.SetText(p.entries[i].Username)
			}
			a.modal = modalNone
		}
	}
	focusOnce(gtx, &p.filter)
	return layout.Center.Layout(gtx, a.narrow(560, a.panel(func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(560))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(a.row(a.iconDisc(icoLock, a.pal.Mauve, 34), a.heading("Vault entries"))),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(a.editor(&p.filter, "filter", true)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Flexed(1, func(gtx C) D {
				ls := material.List(a.th, &p.list)
				ls.Indicator.Color = a.pal.Surface2
				return ls.Layout(gtx, len(vis), func(gtx C, n int) D {
					i := vis[n]
					e := p.entries[i]
					return a.listRow(&p.clicks[i], false, a.row(a.icon(icoKey, a.pal.Yellow, unit.Sp(13)), a.text(e.Title, a.pal.Text), a.small(e.Username, a.pal.Subtext0), a.small(e.URL, a.pal.Overlay1)))(gtx)
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(a.btn(&p.cancel, icoTimes, "Cancel", btnSecondary)),
		)
	})))
}

// listRow is a hoverable, optionally selected full-width row.
func (a *App) listRow(c *widget.Clickable, selected bool, w layout.Widget) layout.Widget {
	return func(gtx C) D {
		return c.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := color.NRGBA{}
			if selected {
				bg = alpha(a.pal.Mauve, 0x30)
			} else if c.Hovered() {
				bg = alpha(a.pal.Surface0, 0x80)
			}
			return a.box(gtx, bg, color.NRGBA{}, unit.Dp(6), layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(8)}, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				return w(gtx)
			})
		})
	}
}
