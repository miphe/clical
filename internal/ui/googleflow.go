package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

// Adding a calendar starts with a choice of source (the chooser). An iCal
// link goes to the add form (form.go); a Google account goes through the
// steps below, each a popup:
//
//	client  — the first time only: the user's OAuth client ID and secret
//	waiting — the browser is open on Google's sign-in page; the full address
//	          is shown too, in case it didn't open
//	pick    — tick which of the account's calendars to add
//
// A typed-in client is never saved by this flow: it's handed to the sign-in,
// and the app saves it only once Google accepts it. So esc at any step, or a
// rejected client, leaves nothing behind, and the form comes back next time.
// `c` while signing in reopens the form (prefilled with the client ID in
// use) to correct or replace the client.
//
// `s` in the sidebar runs the same sign-in for an account whose access
// expired ("reauth"), then syncs it.

const signInTimeout = 10 * time.Minute

type googleStep int

const (
	gChecking googleStep = iota // is an OAuth client saved?
	gClient
	gStarting
	gWaiting
	gListing
	gPick
	gSaving
)

type googleFlow struct {
	step   googleStep
	err    string
	expect string // set when signing an existing account in again

	inputs  [2]textinput.Model // client ID, client secret
	focus   int
	savedID string            // the saved client's ID, "" if none
	client  *app.GoogleClient // typed in, not saved; used for this sign-in only

	signIn app.SignIn
	acct   domain.Account
	cals   []app.RemoteCalendar
	picked []bool
	cursor int
}

type (
	googleConfiguredMsg struct {
		savedID string
		err     error
	}
	signInStartedMsg struct {
		signIn app.SignIn
		err    error
	}
	signInDoneMsg struct {
		acct domain.Account
		err  error
	}
	googleCalendarsMsg struct {
		cals []app.RemoteCalendar
		err  error
	}
	googleAddedMsg struct {
		cals []app.CalendarInfo
		err  error
	}
)

// The chooser.

const (
	sourceICal = iota
	sourceGoogle
)

func (m Model) handleChooserKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.chooser = nil
	case "j", "down", "k", "up", "tab":
		*m.chooser = 1 - *m.chooser
	case "1":
		*m.chooser = sourceICal
	case "2":
		*m.chooser = sourceGoogle
	case "enter":
		choice := *m.chooser
		m.chooser = nil
		if choice == sourceICal {
			return m.openICalForm()
		}
		return m.startGoogleFlow("")
	}
	return m, nil
}

func (m Model) renderChooser() []string {
	rows := []struct{ name, desc string }{
		{"iCal link", "paste a calendar's secret iCal address"},
		{"Google account", "sign in with Google (also work accounts)"},
	}
	var lines []string
	for i, r := range rows {
		text := fmt.Sprintf(" %d  %-15s %s", i+1, r.name, r.desc)
		if i == *m.chooser {
			lines = append(lines, selectedStyle.Render("▸"+text))
		} else {
			lines = append(lines, " "+text)
		}
	}
	lines = append(lines, "", " "+mutedStyle.Render("enter choose · esc cancel"))
	w := min(m.width-4, 66)
	return panel(w, len(lines)+2, frame{focused: true, tabs: []tab{{label: "Add a calendar", active: true}}}, lines)
}

// The Google flow.

// startGoogleFlow opens the flow (expect: an account to sign in again). It
// first checks for a saved OAuth client: with one it goes straight to
// signing in, without one it asks for it.
func (m Model) startGoogleFlow(expect string) (tea.Model, tea.Cmd) {
	m.gflow = &googleFlow{expect: expect, step: gChecking}
	src := m.src
	return m, func() tea.Msg {
		id, err := src.SavedGoogleClientID()
		return googleConfiguredMsg{savedID: id, err: err}
	}
}

// beginSignInCmd starts a sign-in with the typed-in client if there is one,
// otherwise the saved one.
func (m Model) beginSignInCmd() tea.Cmd {
	src, expect, client := m.src, m.gflow.expect, m.gflow.client
	return func() tea.Msg {
		s, err := src.BeginGoogleSignIn(expect, client)
		return signInStartedMsg{signIn: s, err: err}
	}
}

// openClientForm shows the client form with id prefilled (the secret is
// always typed afresh) and msg as its error, if any.
func (g *googleFlow) openClientForm(id, msg string) tea.Cmd {
	if g.signIn != nil {
		g.signIn.Cancel()
		g.signIn = nil
	}
	g.step, g.err = gClient, msg
	g.inputs = newClientInputs()
	g.inputs[0].SetValue(id)
	if id != "" {
		return g.focusClient(1)
	}
	return g.focusClient(0)
}

func (g *googleFlow) clientID() string {
	if g.client != nil {
		return g.client.ID
	}
	return g.savedID
}

func newClientInputs() [2]textinput.Model {
	var in [2]textinput.Model
	for i := range in {
		in[i] = textinput.New()
		in[i].SetStyles(inputStyles())
		in[i].Prompt = ""
		in[i].CharLimit = 512
	}
	in[0].Placeholder = "…apps.googleusercontent.com"
	in[1].Placeholder = "GOCSPX-…"
	in[1].EchoMode = textinput.EchoPassword
	in[1].EchoCharacter = '•'
	return in
}

// googleMsg handles the flow's command results. Results that arrive after
// the flow was closed (esc) are dropped.
func (m Model) googleMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	g := m.gflow
	if g == nil {
		if done, ok := msg.(signInStartedMsg); ok && done.signIn != nil {
			done.signIn.Cancel()
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case googleConfiguredMsg:
		if msg.err != nil {
			return m, g.openClientForm("", msg.err.Error())
		}
		g.savedID = msg.savedID
		if g.savedID == "" {
			return m, g.openClientForm("", "")
		}
		g.step = gStarting
		return m, m.beginSignInCmd()

	case signInStartedMsg:
		if msg.err != nil {
			if g.client != nil {
				return m, g.openClientForm(g.client.ID, msg.err.Error())
			}
			g.step, g.err = gWaiting, msg.err.Error()
			return m, nil
		}
		g.step, g.signIn = gWaiting, msg.signIn
		s, open := msg.signIn, m.openURL
		return m, tea.Batch(
			func() tea.Msg { _ = open(s.URL()); return nil },
			func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), signInTimeout)
				defer cancel()
				acct, err := s.Wait(ctx)
				return signInDoneMsg{acct: acct, err: err}
			},
		)

	case signInDoneMsg:
		if errors.Is(msg.err, app.ErrGoogleClientRejected) {
			g.signIn = nil
			return m, g.openClientForm(g.clientID(), "Google rejected the client ID or secret. Check them and try again.")
		}
		if msg.err != nil {
			g.err = "Sign-in didn't complete: " + msg.err.Error()
			g.signIn = nil
			return m, nil
		}
		if g.expect != "" {
			// Signed in again: sync the account's calendars and carry on.
			m.gflow = nil
			delete(m.needsSignIn, msg.acct.ID)
			m.syncing = true
			return m, tea.Batch(m.syncAccountCmds(msg.acct.ID)...)
		}
		g.acct, g.step = msg.acct, gListing
		src, acct := m.src, msg.acct
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
			defer cancel()
			cals, err := src.GoogleCalendars(ctx, acct)
			return googleCalendarsMsg{cals: cals, err: err}
		}

	case googleCalendarsMsg:
		if msg.err != nil {
			g.step, g.err = gPick, msg.err.Error()
			return m, nil
		}
		g.step, g.cals = gPick, msg.cals
		g.picked = make([]bool, len(msg.cals))
		for i, c := range msg.cals {
			g.picked[i] = c.Primary && !c.Added
		}
		return m, nil

	case googleAddedMsg:
		if msg.err != nil {
			g.step, g.err = gPick, msg.err.Error()
			return m, nil
		}
		m.gflow = nil
		if len(msg.cals) > 0 {
			m.side.sel = sideRow{kind: sideCalendar, cal: msg.cals[0]}.key()
		}
		m.syncing = true
		cmds := []tea.Cmd{m.loadCalendars()}
		for _, c := range msg.cals {
			cmds = append(cmds, m.syncCalendarCmd(c))
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

// syncAccountCmds syncs every calendar of one account.
func (m Model) syncAccountCmds(accountID string) []tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range m.calendars {
		if c.Account.ID == accountID {
			cmds = append(cmds, m.syncCalendarCmd(c))
		}
	}
	return cmds
}

func (g *googleFlow) focusClient(i int) tea.Cmd {
	g.focus = (i + 2) % 2
	var cmd tea.Cmd
	for j := range g.inputs {
		if j == g.focus {
			cmd = g.inputs[j].Focus()
		} else {
			g.inputs[j].Blur()
		}
	}
	return cmd
}

func (m Model) handleGoogleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	g := m.gflow
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if g.signIn != nil {
			g.signIn.Cancel()
		}
		m.gflow = nil
		return m, nil
	}

	switch g.step {
	case gClient:
		switch msg.String() {
		case "tab", "down", "shift+tab", "up":
			return m, g.focusClient(g.focus + 1)
		case "enter":
			if g.focus == 0 {
				return m, g.focusClient(1)
			}
			c, err := app.ValidateGoogleClient(app.GoogleClient{ID: g.inputs[0].Value(), Secret: g.inputs[1].Value()})
			if err != nil {
				g.err = err.Error()
				return m, nil
			}
			g.client, g.step, g.err = &c, gStarting, ""
			return m, m.beginSignInCmd()
		}
		var cmd tea.Cmd
		g.inputs[g.focus], cmd = g.inputs[g.focus].Update(msg)
		g.err = ""
		return m, cmd

	case gWaiting:
		switch {
		case msg.String() == "o" && g.signIn != nil:
			s, open := g.signIn, m.openURL
			return m, func() tea.Msg { _ = open(s.URL()); return nil }
		case msg.String() == "r" && g.signIn == nil: // retry after a failure
			g.err = ""
			g.step = gStarting
			return m, m.beginSignInCmd()
		case msg.String() == "c": // correct or replace the client
			return m, g.openClientForm(g.clientID(), "")
		}

	case gPick:
		switch msg.String() {
		case "j", "down":
			g.cursor = min(g.cursor+1, max(len(g.cals)-1, 0))
		case "k", "up":
			g.cursor = max(g.cursor-1, 0)
		case "space":
			if g.cursor < len(g.cals) && !g.cals[g.cursor].Added {
				g.picked[g.cursor] = !g.picked[g.cursor]
			}
		case "enter":
			var picks []app.RemoteCalendar
			for i, c := range g.cals {
				if g.picked[i] {
					picks = append(picks, c)
				}
			}
			if len(picks) == 0 {
				g.err = "Tick at least one calendar with space."
				return m, nil
			}
			g.step, g.err = gSaving, ""
			src, acct := m.src, g.acct
			return m, func() tea.Msg {
				cals, err := src.AddGoogleCalendars(context.Background(), acct, picks)
				return googleAddedMsg{cals: cals, err: err}
			}
		}
	}
	return m, nil
}

func (m Model) googlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	g := m.gflow
	if g.step != gClient {
		return m, nil
	}
	var cmd tea.Cmd
	g.inputs[g.focus], cmd = g.inputs[g.focus].Update(msg)
	return m, cmd
}

// Rendering.

func (m Model) renderGoogleFlow() []string {
	g := m.gflow
	w := min(m.width-4, 72)
	inner := w - 4
	title := "Add a Google account"
	if g.expect != "" {
		title = "Sign in again · " + g.expect
	}
	var lines []string
	add := func(s string) { lines = append(lines, " "+s) }
	wrap := func(s string, style lipgloss.Style) {
		for _, l := range strings.Split(lipgloss.NewStyle().Width(inner).Render(s), "\n") {
			add(style.Render(l))
		}
	}

	switch g.step {
	case gChecking, gStarting, gListing, gSaving:
		add(busyStyle.Render(map[googleStep]string{
			gChecking: "Checking setup…", gStarting: "Starting sign-in…",
			gListing: "Fetching your calendars…", gSaving: "Adding calendars…",
		}[g.step]))

	case gClient:
		wrap("clical signs in with your own OAuth client, a \"Desktop app\" client from your Google Cloud project. It's saved in the encrypted vault once a sign-in with it succeeds.", mutedStyle)
		lines = append(lines, "")
		for i, label := range []string{"Client ID", "Client secret"} {
			style := mutedStyle
			if i == g.focus {
				style = boldStyle
			}
			g.inputs[i].SetWidth(inner - 1)
			add(style.Render(label))
			add(g.inputs[i].View())
			lines = append(lines, "")
		}

	case gWaiting:
		if g.signIn == nil {
			break
		}
		wrap("Your browser should open on Google's sign-in page. If it doesn't, open this address (o tries again):", mutedStyle)
		lines = append(lines, "")
		addr := g.signIn.URL()
		start, end := hostSpan(addr)
		for _, chunk := range strings.Split(ansi.Hardwrap(addr, inner, false), "\n") {
			// Bold the host where it falls in this chunk.
			i := strings.Index(addr, chunk)
			a, b := max(start, i)-i, min(end, i+len(chunk))-i
			if i >= 0 && a < b {
				chunk = chunk[:a] + boldStyle.Render(chunk[a:b]) + chunk[b:]
			}
			add(ansi.SetHyperlink(addr) + chunk + ansi.ResetHyperlink())
		}
		lines = append(lines, "")
		add(busyStyle.Render("Waiting for you to sign in…"))
		wrap("If Google shows an error about the app or client, press c to enter the client again.", mutedStyle)

	case gPick:
		add("Choose calendars for " + boldStyle.Render(g.acct.Label) + ":")
		lines = append(lines, "")
		maxRows := max(m.bodyHeight()-12, 3)
		first := max(0, min(g.cursor-maxRows+1, len(g.cals)-maxRows))
		for i := first; i < len(g.cals) && i < first+maxRows; i++ {
			c := g.cals[i]
			box := "[ ]"
			if g.picked[i] {
				box = "[x]"
			}
			note := ""
			switch {
			case c.Added:
				box, note = " · ", "  (already added)"
			case c.Primary:
				note = "  (primary)"
			case c.FreeBusyOnly:
				note = "  (free/busy only)"
			}
			text := fit(fmt.Sprintf("%s %s", box, c.Name)+mutedStyle.Render(note), inner-2)
			if i == g.cursor {
				add(selectedStyle.Render("▸ " + ansi.Strip(text)))
			} else {
				add("  " + text)
			}
		}
		if len(g.cals) == 0 && g.err == "" {
			add(mutedStyle.Render("This account has no calendars."))
		}
	}

	if g.err != "" {
		lines = append(lines, "")
		wrap(g.err, errorStyle)
	}
	lines = append(lines, "")
	add(mutedStyle.Render(g.hints()))

	return panel(w, len(lines)+2, frame{focused: true, tabs: []tab{{label: title, active: true}}}, lines)
}

// hints are the popup's own key hints for its current step.
func (g *googleFlow) hints() string {
	switch {
	case g.step == gClient:
		return "enter next/sign in · tab move · esc cancel"
	case g.step == gPick:
		return "space tick · enter add · esc cancel"
	case g.step == gWaiting && g.signIn == nil:
		return "r try again · c change client · esc cancel"
	case g.step == gWaiting:
		return "o open again · c change client · esc cancel"
	}
	return "esc cancel"
}
