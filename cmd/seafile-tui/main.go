package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Views
const (
	viewLogin    = "login"
	viewRepos    = "repos"
	viewNewRepo  = "new_repo"
	viewConfirm  = "confirm_delete"
)

// Styles
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// Messages
type loginDoneMsg struct{ err error }
type reposLoadedMsg struct {
	repos []Repo
	err   error
}
type repoCreatedMsg struct{ err error }
type repoDeletedMsg struct{ err error }

type model struct {
	client *APIClient
	view   string

	// Login
	emailInput    textinput.Model
	passwordInput textinput.Model
	loginFocus    int // 0=email, 1=password

	// Repos
	repos    []Repo
	cursor   int
	message  string // status message

	// New repo
	newRepoInput textinput.Model

	width  int
	height int
}

func initialModel(serverURL string) model {
	email := textinput.New()
	email.Placeholder = "email@example.com"
	email.Focus()
	email.CharLimit = 255

	password := textinput.New()
	password.Placeholder = "password"
	password.EchoMode = textinput.EchoPassword
	password.CharLimit = 255

	newRepo := textinput.New()
	newRepo.Placeholder = "Library name"
	newRepo.CharLimit = 255

	return model{
		client:        NewClient(serverURL),
		view:          viewLogin,
		emailInput:    email,
		passwordInput: password,
		newRepoInput:  newRepo,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.view == viewLogin || m.view == viewRepos {
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	switch m.view {
	case viewLogin:
		return m.updateLogin(msg)
	case viewRepos:
		return m.updateRepos(msg)
	case viewNewRepo:
		return m.updateNewRepo(msg)
	case viewConfirm:
		return m.updateConfirm(msg)
	}

	return m, nil
}

// --- Login View ---

func (m model) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab", "down", "up":
			m.loginFocus = (m.loginFocus + 1) % 2
			if m.loginFocus == 0 {
				m.emailInput.Focus()
				m.passwordInput.Blur()
			} else {
				m.emailInput.Blur()
				m.passwordInput.Focus()
			}
			return m, nil

		case "enter":
			email := m.emailInput.Value()
			password := m.passwordInput.Value()
			if email == "" || password == "" {
				m.message = "Email and password required"
				return m, nil
			}
			m.message = "Logging in..."
			return m, func() tea.Msg {
				err := m.client.Login(email, password)
				return loginDoneMsg{err: err}
			}
		}

	case loginDoneMsg:
		if msg.err != nil {
			m.message = errorStyle.Render(msg.err.Error())
			return m, nil
		}
		m.view = viewRepos
		m.message = ""
		return m, m.loadRepos
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.emailInput, cmd = m.emailInput.Update(msg)
	cmds = append(cmds, cmd)
	m.passwordInput, cmd = m.passwordInput.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m model) renderLogin() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Seafile Login") + "\n\n")
	b.WriteString("Email:\n")
	b.WriteString(m.emailInput.View() + "\n\n")
	b.WriteString("Password:\n")
	b.WriteString(m.passwordInput.View() + "\n\n")
	if m.message != "" {
		b.WriteString(m.message + "\n\n")
	}
	b.WriteString(helpStyle.Render("tab: switch field  enter: login  q: quit"))
	return b.String()
}

// --- Repos View ---

func (m model) loadRepos() tea.Msg {
	repos, err := m.client.ListRepos()
	return reposLoadedMsg{repos: repos, err: err}
}

func (m model) updateRepos(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.repos)-1 {
				m.cursor++
			}
		case "n":
			m.view = viewNewRepo
			m.newRepoInput.SetValue("")
			m.newRepoInput.Focus()
			m.message = ""
			return m, textinput.Blink
		case "d":
			if len(m.repos) > 0 {
				m.view = viewConfirm
				m.message = ""
			}
		case "r":
			m.message = "Refreshing..."
			return m, m.loadRepos
		}

	case reposLoadedMsg:
		if msg.err != nil {
			m.message = errorStyle.Render(msg.err.Error())
			return m, nil
		}
		m.repos = msg.repos
		m.message = ""
		if m.cursor >= len(m.repos) {
			m.cursor = max(0, len(m.repos)-1)
		}
	}

	return m, nil
}

func (m model) renderRepos() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Libraries") + "\n\n")

	if len(m.repos) == 0 {
		b.WriteString(dimStyle.Render("  No libraries yet. Press 'n' to create one.") + "\n")
	}

	for i, repo := range m.repos {
		cursor := "  "
		name := repo.Name
		if repo.Name == "" {
			name = "(unnamed)"
		}
		if i == m.cursor {
			cursor = "> "
			name = selectedStyle.Render(name)
		}
		ts := ""
		if repo.UpdateTime > 0 {
			ts = dimStyle.Render(" " + time.Unix(repo.UpdateTime, 0).Format("2006-01-02 15:04"))
		}
		encrypted := ""
		if repo.Encrypted {
			encrypted = dimStyle.Render(" [encrypted]")
		}
		b.WriteString(fmt.Sprintf("%s%s%s%s\n", cursor, name, ts, encrypted))
		b.WriteString(dimStyle.Render(fmt.Sprintf("    %s", repo.ID)) + "\n")
	}

	b.WriteString("\n")
	if m.message != "" {
		b.WriteString(m.message + "\n")
	}
	b.WriteString(helpStyle.Render("j/k: navigate  n: new  d: delete  r: refresh  q: quit"))
	return b.String()
}

// --- New Repo View ---

func (m model) updateNewRepo(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.view = viewRepos
			return m, nil
		case "enter":
			name := m.newRepoInput.Value()
			if name == "" {
				m.message = "Name is required"
				return m, nil
			}
			m.message = "Creating..."
			return m, func() tea.Msg {
				_, err := m.client.CreateRepo(name)
				return repoCreatedMsg{err: err}
			}
		}

	case repoCreatedMsg:
		if msg.err != nil {
			m.message = errorStyle.Render(msg.err.Error())
			return m, nil
		}
		m.view = viewRepos
		m.message = successStyle.Render("Library created")
		return m, m.loadRepos
	}

	var cmd tea.Cmd
	m.newRepoInput, cmd = m.newRepoInput.Update(msg)
	return m, cmd
}

func (m model) renderNewRepo() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Create Library") + "\n\n")
	b.WriteString("Name:\n")
	b.WriteString(m.newRepoInput.View() + "\n\n")
	if m.message != "" {
		b.WriteString(m.message + "\n\n")
	}
	b.WriteString(helpStyle.Render("enter: create  esc: cancel"))
	return b.String()
}

// --- Confirm Delete View ---

func (m model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			repo := m.repos[m.cursor]
			m.message = "Deleting..."
			return m, func() tea.Msg {
				err := m.client.DeleteRepo(repo.ID)
				return repoDeletedMsg{err: err}
			}
		case "n", "N", "esc":
			m.view = viewRepos
			return m, nil
		}

	case repoDeletedMsg:
		if msg.err != nil {
			m.view = viewRepos
			m.message = errorStyle.Render(msg.err.Error())
			return m, nil
		}
		m.view = viewRepos
		m.message = successStyle.Render("Library deleted")
		return m, m.loadRepos
	}

	return m, nil
}

func (m model) renderConfirm() string {
	var b strings.Builder
	name := "(unnamed)"
	if m.cursor < len(m.repos) && m.repos[m.cursor].Name != "" {
		name = m.repos[m.cursor].Name
	}
	b.WriteString(titleStyle.Render("Delete Library") + "\n\n")
	b.WriteString(fmt.Sprintf("Are you sure you want to delete %q?\n\n", name))
	b.WriteString(helpStyle.Render("y: yes  n: no"))
	return b.String()
}

// --- View dispatch ---

func (m model) View() string {
	switch m.view {
	case viewLogin:
		return m.renderLogin()
	case viewRepos:
		return m.renderRepos()
	case viewNewRepo:
		return m.renderNewRepo()
	case viewConfirm:
		return m.renderConfirm()
	}
	return ""
}

func main() {
	serverURL := "http://localhost:8082"
	if len(os.Args) > 1 {
		serverURL = os.Args[1]
	}

	p := tea.NewProgram(initialModel(serverURL), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
