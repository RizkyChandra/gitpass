package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RizkyChandra/gitpass/internal/sync"
	"github.com/RizkyChandra/gitpass/internal/vault"
)

// clipboardTTL is how long a copied secret is left on the clipboard.
const clipboardTTL = 30 * time.Second

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(10)
	valueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	codeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

type mode int

const (
	modeList mode = iota
	modeDetail
	modeEdit
)

// field order in the edit form
var fieldNames = []string{"Name", "Username", "Email", "Password", "TOTP", "URL", "Tags", "Notes"}

type item struct{ e vault.Entry }

func (i item) Title() string { return i.e.Name }
func (i item) Description() string {
	who := i.e.Username
	if who == "" {
		who = i.e.Email
	}
	if len(i.e.Tags) > 0 {
		who += "  [" + strings.Join(i.e.Tags, " ") + "]"
	}
	return who
}
func (i item) FilterValue() string {
	return strings.Join([]string{i.e.Name, i.e.Username, i.e.Email, i.e.URL, strings.Join(i.e.Tags, " ")}, " ")
}

type (
	tickMsg time.Time
	syncMsg struct {
		res sync.Result
		err error
	}
	clearClipMsg struct{ value string }
	errMsg       struct{ err error }
	statusMsg    string
)

type model struct {
	v       *vault.Vault
	list    list.Model
	mode    mode
	cur     vault.Entry
	editing vault.Entry // entry being edited; zero ID means a new one
	inputs  []textinput.Model
	focus   int
	reveal  bool
	confirm bool // delete confirmation pending
	status  string
	isErr   bool
	syncing bool
	now     time.Time
	w, h    int
}

func runTUI() error {
	v, err := open()
	if err != nil {
		return err
	}
	m, err := newModel(v)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func newModel(v *vault.Vault) (*model, error) {
	m := &model{v: v, now: time.Now()}
	delegate := list.NewDefaultDelegate()
	m.list = list.New(nil, delegate, 0, 0)
	m.list.Title = "gitpass"
	m.list.Styles.Title = titleStyle
	m.list.SetShowStatusBar(false)
	m.list.SetShowHelp(false)
	if err := m.reload(); err != nil {
		return nil, err
	}
	return m, nil
}

// reload rebuilds the list from disk. Called after every mutation and sync.
func (m *model) reload() error {
	entries, err := m.v.List()
	if err != nil {
		return err
	}
	items := make([]list.Item, len(entries))
	for i, e := range entries {
		items[i] = item{e}
	}
	m.list.SetItems(items) // the returned Cmd only drives the filter status line
	return nil
}

func (m *model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.list.SetSize(msg.Width, msg.Height-2)
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		return m, tick()

	case syncMsg:
		m.syncing = false
		if msg.err != nil {
			return m, m.fail(msg.err)
		}
		if err := m.reload(); err != nil {
			return m, m.fail(err)
		}
		m.setStatus(msg.res.String())
		return m, nil

	case clearClipMsg:
		// Only clear if nobody else has since written to the clipboard.
		if cur, err := clipboard.ReadAll(); err == nil && cur == msg.value {
			_ = clipboard.WriteAll("")
			m.setStatus("clipboard cleared")
		}
		return m, nil

	case statusMsg:
		m.setStatus(string(msg))
		return m, nil

	case errMsg:
		m.isErr, m.status = true, msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeDetail:
			return m.updateDetail(msg)
		case modeEdit:
			return m.updateEdit(msg)
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *model) setStatus(s string) { m.status, m.isErr = s, false }

func (m *model) fail(err error) tea.Cmd {
	return func() tea.Msg { return errMsg{err} }
}

func (m *model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the filter is being typed, every key belongs to the filter.
	if m.list.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter":
		if it, ok := m.list.SelectedItem().(item); ok {
			m.cur, m.mode, m.reveal = it.e, modeDetail, false
		}
		return m, nil
	case "a":
		m.startEdit(vault.Entry{})
		return m, nil
	case "s":
		return m, m.startSync()
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirm {
		m.confirm = false
		if msg.String() == "y" {
			if err := m.v.Delete(m.cur.ID); err != nil {
				return m, m.fail(err)
			}
			if err := m.reload(); err != nil {
				return m, m.fail(err)
			}
			m.mode = modeList
			m.setStatus("deleted " + m.cur.Name)
			return m, nil
		}
		m.setStatus("delete cancelled")
		return m, nil
	}

	switch msg.String() {
	case "esc", "q":
		m.mode = modeList
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case " ":
		m.reveal = !m.reveal
		return m, nil
	case "p":
		return m, m.copy("password", m.cur.Password)
	case "u":
		who := m.cur.Username
		if who == "" {
			who = m.cur.Email
		}
		return m, m.copy("username", who)
	case "t":
		code, _, err := m.cur.Code(m.now)
		if err != nil {
			return m, m.fail(err)
		}
		return m, m.copy("code", code)
	case "e":
		m.startEdit(m.cur)
		return m, nil
	case "d":
		m.confirm = true
		m.setStatus("delete " + m.cur.Name + "? (y/n)")
		return m, nil
	case "s":
		return m, m.startSync()
	}
	return m, nil
}

// copy puts a secret on the clipboard and schedules it to be wiped.
func (m *model) copy(what, value string) tea.Cmd {
	if value == "" {
		return m.fail(fmt.Errorf("no %s set", what))
	}
	if err := clipboard.WriteAll(value); err != nil {
		return m.fail(err)
	}
	m.setStatus(fmt.Sprintf("%s copied — clears in %ds", what, int(clipboardTTL.Seconds())))
	return tea.Tick(clipboardTTL, func(time.Time) tea.Msg { return clearClipMsg{value} })
}

func (m *model) startSync() tea.Cmd {
	if m.syncing {
		return nil
	}
	m.syncing = true
	m.setStatus("syncing…")
	v := m.v
	return func() tea.Msg {
		res, err := sync.Sync(v)
		return syncMsg{res, err}
	}
}

func (m *model) startEdit(e vault.Entry) {
	m.editing, m.mode, m.focus = e, modeEdit, 0
	values := []string{e.Name, e.Username, e.Email, e.Password, e.TOTP, e.URL, strings.Join(e.Tags, " "), e.Notes}
	m.inputs = make([]textinput.Model, len(fieldNames))
	for i := range fieldNames {
		in := textinput.New()
		in.SetValue(values[i])
		in.Prompt = ""
		in.CharLimit = 4096
		m.inputs[i] = in
	}
	m.inputs[0].Focus()
}

func (m *model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		if m.editing.ID != "" {
			m.mode = modeDetail
		}
		m.setStatus("edit cancelled")
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "tab", "down":
		m.focusField(m.focus + 1)
		return m, nil
	case "shift+tab", "up":
		m.focusField(m.focus - 1)
		return m, nil
	case "ctrl+g":
		p, err := vault.RandomPassword(20)
		if err != nil {
			return m, m.fail(err)
		}
		m.inputs[3].SetValue(p)
		m.setStatus("generated a 20-character password")
		return m, nil
	case "ctrl+s":
		return m, m.save()
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m *model) focusField(i int) {
	m.inputs[m.focus].Blur()
	m.focus = (i + len(m.inputs)) % len(m.inputs)
	m.inputs[m.focus].Focus()
}

func (m *model) save() tea.Cmd {
	e := m.editing
	e.Name = strings.TrimSpace(m.inputs[0].Value())
	e.Username = m.inputs[1].Value()
	e.Email = m.inputs[2].Value()
	e.Password = m.inputs[3].Value()
	e.TOTP = strings.TrimSpace(m.inputs[4].Value())
	e.URL = m.inputs[5].Value()
	e.Tags = strings.Fields(m.inputs[6].Value())
	e.Notes = m.inputs[7].Value()

	saved, err := m.v.Put(e)
	if err != nil {
		return m.fail(err)
	}
	if err := m.reload(); err != nil {
		return m.fail(err)
	}
	m.cur, m.mode = saved, modeDetail
	m.setStatus("saved " + saved.Name)
	return nil
}

func (m *model) View() string {
	var body string
	switch m.mode {
	case modeList:
		body = m.list.View() + "\n" + helpStyle.Render("enter open · a add · / filter · s sync · q quit")
	case modeDetail:
		body = m.viewDetail()
	case modeEdit:
		body = m.viewEdit()
	}
	return body + "\n" + m.viewStatus()
}

func (m *model) viewStatus() string {
	if m.status == "" {
		return ""
	}
	if m.isErr {
		return errorStyle.Render("! " + m.status)
	}
	return statusStyle.Render(m.status)
}

func (m *model) viewDetail() string {
	var b strings.Builder
	e := m.cur
	b.WriteString(titleStyle.Render(e.Name) + "\n\n")

	row := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(labelStyle.Render(label) + valueStyle.Render(value) + "\n")
	}
	row("user", e.Username)
	row("email", e.Email)

	if e.Password != "" {
		shown := strings.Repeat("•", 12)
		if m.reveal {
			shown = e.Password
		}
		row("password", shown)
	}
	if e.TOTP != "" {
		code, left, err := e.Code(m.now)
		if err != nil {
			b.WriteString(labelStyle.Render("totp") + errorStyle.Render(err.Error()) + "\n")
		} else {
			bar := strings.Repeat("█", left/2) + strings.Repeat("░", 15-left/2)
			b.WriteString(labelStyle.Render("totp") +
				codeStyle.Render(code) +
				statusStyle.Render(fmt.Sprintf("  %s %2ds", bar, left)) + "\n")
		}
	}
	row("url", e.URL)
	if len(e.Tags) > 0 {
		row("tags", strings.Join(e.Tags, " "))
	}
	if e.Notes != "" {
		b.WriteString("\n" + valueStyle.Render(e.Notes) + "\n")
	}
	b.WriteString("\n" + statusStyle.Render("updated "+e.UpdatedAt.Local().Format("2006-01-02 15:04")) + "\n")
	b.WriteString("\n" + helpStyle.Render("p password · u user · t code · space reveal · e edit · d delete · s sync · esc back"))
	return b.String()
}

func (m *model) viewEdit() string {
	var b strings.Builder
	what := "new entry"
	if m.editing.ID != "" {
		what = "editing " + m.editing.Name
	}
	b.WriteString(titleStyle.Render(what) + "\n\n")
	for i, name := range fieldNames {
		cursor := "  "
		if i == m.focus {
			cursor = "> "
		}
		b.WriteString(cursor + labelStyle.Render(strings.ToLower(name)) + m.inputs[i].View() + "\n")
	}
	b.WriteString("\n" + helpStyle.Render("tab next · ctrl+g generate password · ctrl+s save · esc cancel"))
	return b.String()
}
