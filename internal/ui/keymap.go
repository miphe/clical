package ui

import "charm.land/bubbles/v2/key"

// keyMap holds every binding in one place so the footer hints (and later the
// help modal and YAML remapping) share a single source of truth.
//
// Panes are reached the same way from anywhere (outside popups): 0 is the
// calendar list, 1/2/3 the main view as Agenda/Week/Month, and tab switches
// between the two panes. Each pane's number is shown in its title. Keys
// inside a pane only act within it: h/l never move focus to another pane.
type keyMap struct {
	Up, Down, Top, Bottom key.Binding
	PrevDay, NextDay      key.Binding
	Today                 key.Binding
	Open, Close           key.Binding
	Left, Right           key.Binding // within a pane
	FocusList             key.Binding
	SwitchPane            key.Binding
	Back                  key.Binding // calendar list → main view (alias of tab there)
	Toggle                key.Binding
	Collapse              key.Binding
	Sync                  key.Binding
	ViewAgenda, ViewWeek  key.Binding
	ViewMonth             key.Binding
	Add, Remove, Rename   key.Binding
	Quit                  key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up:         key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:       key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Top:        key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:     key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		PrevDay:    key.NewBinding(key.WithKeys("[", "pgup"), key.WithHelp("[", "prev day")),
		NextDay:    key.NewBinding(key.WithKeys("]", "pgdown"), key.WithHelp("]", "next day")),
		Today:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "today")),
		Open:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Close:      key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc", "close")),
		Left:       key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h", "left")),
		Right:      key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l", "right")),
		FocusList:  key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "calendars")),
		SwitchPane: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		Back:       key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l", "back")),
		Toggle:     key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "show/hide")),
		Collapse:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "collapse")),
		Sync:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "sync")),
		ViewAgenda: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "agenda")),
		ViewWeek:   key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "week")),
		ViewMonth:  key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "month")),
		Add:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Remove:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
		Rename:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "rename")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}
