package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/spf13/cobra"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

const (
	defaultMuxSessionName = "bastion-mux"
	muxTmuxBinaryName     = "tmux"
	muxListLimit          = 100
	muxRefreshInterval    = 200 * time.Millisecond
)

type muxOptions struct {
	sessionName string
	lookPath    func(string) (string, error)
	executable  func() (string, error)
	getenv      func(string) string
	newRunner   func(string) tmuxCommandRunner
	runTUI      func(context.Context, muxRunOptions) error
}

type muxRunOptions struct {
	sessionName  string
	backend      muxBackend
	environments muxEnvironmentLister
	in           io.Reader
	out          io.Writer
	errOut       io.Writer
}

type muxEnvironmentLister interface {
	ListEnvironments(context.Context, int, string, []string) (services.Page[environment.Environment], error)
}

type muxBackend interface {
	ListSessions(context.Context) ([]muxSession, error)
	OpenEnvironment(context.Context, environment.Environment) (muxSession, error)
	Capture(context.Context, muxSession, int) (string, error)
	Resize(context.Context, muxSession, int, int) error
	SendText(context.Context, muxSession, string) error
	SendKey(context.Context, muxSession, string) error
}

type muxSession struct {
	WindowID       string
	PaneID         string
	WindowName     string
	EnvironmentID  string
	EnvironmentKey string
}

func newMuxCommand(opts *rootOptions) *cobra.Command {
	return newMuxCommandWithOptions(opts, muxOptions{})
}

func newMuxCommandWithOptions(rootOpts *rootOptions, opts muxOptions) *cobra.Command {
	if opts.lookPath == nil {
		opts.lookPath = exec.LookPath
	}

	if opts.executable == nil {
		opts.executable = os.Executable
	}

	if opts.getenv == nil {
		opts.getenv = os.Getenv
	}

	if opts.newRunner == nil {
		opts.newRunner = func(binary string) tmuxCommandRunner {
			return execTmuxRunner{binary: binary}
		}
	}

	if opts.runTUI == nil {
		opts.runTUI = runMuxTUI
	}

	if opts.sessionName == "" {
		opts.sessionName = muxSessionNameFromEnv(opts.getenv)
	}

	sessionName := opts.sessionName

	cmd := &cobra.Command{
		Use:   "mux",
		Short: "Manage persistent SSH sessions in a terminal multiplexer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(sessionName) == "" {
				return errors.New("mux session name cannot be blank")
			}

			tmuxPath, err := opts.lookPath(muxTmuxBinaryName)
			if err != nil {
				return errors.New("tmux is required for bastion mux; install tmux and try again")
			}

			executable, err := opts.executable()
			if err != nil {
				return fmt.Errorf("resolve bastion executable: %w", err)
			}

			api := apiClient(rootOpts)
			backend := tmuxMuxBackend{
				sessionName: sessionName,
				runner:      opts.newRunner(tmuxPath),
				executable:  executable,
				apiURL:      rootOpts.apiURL,
			}

			return opts.runTUI(cmd.Context(), muxRunOptions{
				sessionName:  sessionName,
				backend:      backend,
				environments: api,
				in:           cmd.InOrStdin(),
				out:          cmd.OutOrStdout(),
				errOut:       cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().StringVar(&sessionName, "session", sessionName, "tmux session name used by bastion mux")

	return cmd
}

func muxSessionNameFromEnv(getenv func(string) string) string {
	if value := getenv("BASTION_MUX_SESSION"); value != "" {
		return value
	}

	return defaultMuxSessionName
}

type tmuxCommandRunner interface {
	run(context.Context, ...string) (string, error)
}

type execTmuxRunner struct {
	binary string
}

func (r execTmuxRunner) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.binary, args...) //nolint:gosec // bastion mux intentionally controls tmux.

	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return string(output), fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}

		return string(output), fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, message)
	}

	return string(output), nil
}

type tmuxMuxBackend struct {
	sessionName string
	runner      tmuxCommandRunner
	executable  string
	apiURL      string
}

func (b tmuxMuxBackend) ListSessions(ctx context.Context) ([]muxSession, error) {
	exists, err := b.hasSession(ctx)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, nil
	}

	output, err := b.runner.run(ctx, "list-windows", "-t", b.sessionName, "-F", "#{window_id}\t#{window_name}\t#{pane_id}\t#{@bastion_env_id}\t#{@bastion_env_key}")
	if err != nil {
		return nil, err
	}

	return parseTmuxMuxSessions(output), nil
}

func (b tmuxMuxBackend) OpenEnvironment(ctx context.Context, env environment.Environment) (muxSession, error) {
	sessions, err := b.ListSessions(ctx)
	if err != nil {
		return muxSession{}, err
	}

	if index := slices.IndexFunc(sessions, func(session muxSession) bool { return session.EnvironmentID == env.ID }); index >= 0 {
		return sessions[index], nil
	}

	exists, err := b.hasSession(ctx)
	if err != nil {
		return muxSession{}, err
	}

	label := muxEnvironmentLabel(env)
	command := bastionSSHCommand(b.executable, b.apiURL, env.ID)

	args := []string{"new-window", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-t", b.sessionName, "-n", label, command}
	if !exists {
		args = []string{"new-session", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-s", b.sessionName, "-n", label, command}
	}

	output, err := b.runner.run(ctx, args...)
	if err != nil {
		return muxSession{}, err
	}

	windowID, paneID, err := parseTmuxWindowPane(output)
	if err != nil {
		return muxSession{}, err
	}

	session := muxSession{WindowID: windowID, PaneID: paneID, WindowName: label, EnvironmentID: env.ID}
	if env.Key != nil {
		session.EnvironmentKey = *env.Key
	}

	if err := b.setSessionMetadata(ctx, session); err != nil {
		return muxSession{}, err
	}

	return session, nil
}

func (b tmuxMuxBackend) Capture(ctx context.Context, session muxSession, height int) (string, error) {
	if session.PaneID == "" {
		return "", nil
	}

	if height < 1 {
		height = 24
	}

	return b.runner.run(ctx, "capture-pane", "-p", "-e", "-t", session.PaneID, "-S", "-"+strconv.Itoa(height))
}

func (b tmuxMuxBackend) Resize(ctx context.Context, session muxSession, width, height int) error {
	if session.PaneID == "" || width < 1 || height < 1 {
		return nil
	}

	_, err := b.runner.run(ctx, "resize-pane", "-t", session.PaneID, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))

	return err
}

func (b tmuxMuxBackend) SendText(ctx context.Context, session muxSession, text string) error {
	if session.PaneID == "" || text == "" {
		return nil
	}

	_, err := b.runner.run(ctx, "send-keys", "-t", session.PaneID, "-l", "--", text)

	return err
}

func (b tmuxMuxBackend) SendKey(ctx context.Context, session muxSession, keyName string) error {
	if session.PaneID == "" || keyName == "" {
		return nil
	}

	_, err := b.runner.run(ctx, "send-keys", "-t", session.PaneID, keyName)

	return err
}

func (b tmuxMuxBackend) hasSession(ctx context.Context) (bool, error) {
	_, err := b.runner.run(ctx, "has-session", "-t", b.sessionName)
	if err == nil {
		return true, nil
	}

	if tmuxMissingSessionError(err) {
		return false, nil
	}

	return false, err
}

func tmuxMissingSessionError(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()

	return strings.Contains(message, "can't find session") ||
		strings.Contains(message, "no server running") ||
		strings.Contains(message, "missing")
}

func (b tmuxMuxBackend) setSessionMetadata(ctx context.Context, session muxSession) error {
	metadata := map[string]string{
		"@bastion_env_id":  session.EnvironmentID,
		"@bastion_env_key": session.EnvironmentKey,
		"@bastion_label":   muxSessionLabel(session),
	}

	for key, value := range metadata {
		if _, err := b.runner.run(ctx, "set-window-option", "-t", session.WindowID, key, value); err != nil {
			return err
		}
	}

	return nil
}

func parseTmuxMuxSessions(output string) []muxSession {
	var sessions []muxSession

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 5 || fields[3] == "" {
			continue
		}

		sessions = append(sessions, muxSession{
			WindowID:       fields[0],
			WindowName:     fields[1],
			PaneID:         fields[2],
			EnvironmentID:  fields[3],
			EnvironmentKey: fields[4],
		})
	}

	return sessions
}

func parseTmuxWindowPane(output string) (string, string, error) {
	fields := strings.Split(strings.TrimSpace(output), "\t")
	if len(fields) < 2 || fields[0] == "" || fields[1] == "" {
		return "", "", fmt.Errorf("tmux returned unexpected window response %q", strings.TrimSpace(output))
	}

	return fields[0], fields[1], nil
}

func bastionSSHCommand(executable, apiURL, environmentID string) string {
	return "exec " + shellQuote(executable) + " --api-url " + shellQuote(apiURL) + " ssh --id " + shellQuoteIfNeeded(environmentID)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shellQuoteIfNeeded(value string) string {
	if value == "" {
		return "''"
	}

	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' {
			continue
		}

		return shellQuote(value)
	}

	return value
}

type muxMode int

const (
	muxModeSessions muxMode = iota
	muxModeEnvironmentPicker
)

type muxModel struct {
	context      func() context.Context
	backend      muxBackend
	environments muxEnvironmentLister
	sessionName  string
	sessions     []muxSession
	current      int
	content      string
	status       string
	mode         muxMode
	prefix       bool
	list         list.Model
	width        int
	height       int
}

type muxSessionsMsg struct {
	sessions []muxSession
	err      error
}

type muxCaptureMsg struct {
	paneID  string
	content string
	err     error
}

type muxEnvironmentsMsg struct {
	environments []environment.Environment
	err          error
}

type muxStartedMsg struct {
	session muxSession
	err     error
}

type muxSentKeyMsg struct {
	err error
}

type muxTickMsg time.Time

func runMuxTUI(ctx context.Context, opts muxRunOptions) error {
	zone.NewGlobal()

	defer zone.Close()

	program := tea.NewProgram(newMuxModel(ctx, opts), tea.WithContext(ctx), tea.WithInput(opts.in), tea.WithOutput(opts.out))
	_, err := program.Run()

	return err
}

func newMuxModel(ctx context.Context, opts muxRunOptions) muxModel {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true
	delegate.SetSpacing(0)
	environmentList := list.New(nil, delegate, 80, 20)
	environmentList.Title = "Running Bastion environments"
	environmentList.SetShowStatusBar(false)
	environmentList.SetShowPagination(true)
	environmentList.SetShowHelp(true)
	environmentList.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "connect"))}
	}

	return muxModel{
		context:      func() context.Context { return ctx },
		backend:      opts.backend,
		environments: opts.environments,
		sessionName:  opts.sessionName,
		list:         environmentList,
		width:        80,
		height:       24,
	}
}

func (m muxModel) Init() tea.Cmd {
	return tea.Batch(m.loadSessionsCmd(), m.tickCmd())
}

func (m muxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case muxSessionsMsg:
		return m.handleSessions(msg)

	case muxCaptureMsg:
		return m.handleCapture(msg)

	case muxEnvironmentsMsg:
		return m.handleEnvironments(msg)

	case muxStartedMsg:
		return m.handleStarted(msg)

	case muxSentKeyMsg:
		return m.handleSentKey(msg)

	case muxTickMsg:
		return m, tea.Batch(m.captureCurrentCmd(), m.tickCmd())

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	case tea.KeyPressMsg:
		return m.updateKeyPress(msg)
	}

	return m, nil
}

func (m muxModel) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.list.SetSize(max(20, msg.Width-8), max(5, msg.Height-8))

	return m, m.resizeCurrentCmd()
}

func (m muxModel) handleSessions(msg muxSessionsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}

	m.sessions = msg.sessions
	if m.current >= len(m.sessions) {
		m.current = max(0, len(m.sessions)-1)
	}

	return m, tea.Batch(m.captureCurrentCmd(), m.resizeCurrentCmd())
}

func (m muxModel) handleCapture(msg muxCaptureMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}

	if current := m.currentSession(); current.PaneID == msg.paneID {
		m.content = msg.content
	}

	return m, nil
}

func (m muxModel) handleEnvironments(msg muxEnvironmentsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}

	items := muxEnvironmentListItems(muxSSHConnectableEnvironments(msg.environments))
	m.mode = muxModeEnvironmentPicker
	m.status = ""

	return m, m.list.SetItems(items)
}

func (m muxModel) handleStarted(msg muxStartedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.err.Error()
		return m, nil
	}

	m.mode = muxModeSessions
	m.prefix = false
	m.status = "connected to " + muxSessionLabel(msg.session)
	m.upsertSession(msg.session)

	return m, tea.Batch(m.captureCurrentCmd(), m.resizeCurrentCmd())
}

func (m muxModel) handleSentKey(msg muxSentKeyMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status = msg.err.Error()
	}

	return m, m.captureCurrentCmd()
}

func (m muxModel) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || m.mode != muxModeSessions {
		return m, nil
	}

	for index := range m.sessions {
		if zone.Get(muxTabZone(index)).InBounds(msg) {
			m.current = index
			m.content = ""

			return m, tea.Batch(m.captureCurrentCmd(), m.resizeCurrentCmd())
		}
	}

	return m, nil
}

func (m muxModel) View() tea.View {
	view := tea.NewView(zone.Scan(m.viewContent()))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion

	return view
}

func (m muxModel) updateKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.mode == muxModeEnvironmentPicker {
		return m.updateEnvironmentPickerKey(msg)
	}

	if len(m.sessions) == 0 {
		return m.updateEmptySessionsKey(msg)
	}

	if m.prefix {
		return m.updatePrefixKey(msg)
	}

	if msg.String() == "ctrl+b" {
		m.prefix = true
		m.status = "prefix: n new, h/l switch, d detach"

		return m, nil
	}

	return m, m.sendKeyPressCmd(msg)
}

func (m muxModel) updateEnvironmentPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = muxModeSessions
		m.prefix = false

		return m, nil
	case "enter":
		item, ok := m.list.SelectedItem().(muxEnvironmentItem)
		if !ok {
			return m, nil
		}

		m.status = "connecting to " + muxEnvironmentLabel(item.Environment)

		return m, m.openEnvironmentCmd(item.Environment)
	}

	updatedList, cmd := m.list.Update(msg)
	m.list = updatedList

	return m, cmd
}

func (m muxModel) updateEmptySessionsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "n", "enter":
		m.status = "loading environments"

		return m, m.loadEnvironmentsCmd()
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m muxModel) updatePrefixKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.prefix = false

	switch msg.String() {
	case "n", "c":
		m.status = "loading environments"

		return m, m.loadEnvironmentsCmd()
	case "d", "q":
		return m, tea.Quit
	case "l", "right", "tab":
		m.nextSession()

		return m, tea.Batch(m.captureCurrentCmd(), m.resizeCurrentCmd())
	case "h", "left", "shift+tab":
		m.previousSession()

		return m, tea.Batch(m.captureCurrentCmd(), m.resizeCurrentCmd())
	case "ctrl+b":
		return m, m.sendKeyPressCmd(msg)
	default:
		m.status = "unknown mux command: " + msg.String()

		return m, nil
	}
}

func (m muxModel) viewContent() string {
	width := max(1, m.width)
	height := max(1, m.height)

	pickerHeight := 0
	if m.mode == muxModeEnvironmentPicker {
		pickerHeight = min(max(8, height/2), max(1, height-4))
	}

	bodyHeight := max(1, height-3-pickerHeight)

	tabs := m.viewTabs(width)
	body := m.viewBody(width, bodyHeight)
	sections := []string{tabs, body}

	if m.mode == muxModeEnvironmentPicker {
		sections = append(sections, m.viewEnvironmentPicker(width, pickerHeight))
	}

	sections = append(sections, m.viewStatus(width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m muxModel) viewTabs(width int) string {
	base := lipgloss.NewStyle().Padding(0, 1)
	active := base.Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
	inactive := base.Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236"))

	if len(m.sessions) == 0 {
		return lipgloss.NewStyle().Width(width).Foreground(lipgloss.Color("245")).Render("bastion mux: no SSH sessions")
	}

	parts := make([]string, 0, len(m.sessions))
	for index, session := range m.sessions {
		style := inactive
		if index == m.current {
			style = active
		}

		parts = append(parts, zone.Mark(muxTabZone(index), style.Render(muxSessionLabel(session))))
	}

	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Top, parts...))
}

func (m muxModel) viewBody(width, height int) string {
	bodyStyle := lipgloss.NewStyle().Width(width).Height(height).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
	if len(m.sessions) == 0 {
		return bodyStyle.Render("Press n to create a persistent SSH session. Press q to detach.")
	}

	content := strings.TrimRight(m.content, "\n")
	if content == "" {
		content = "Waiting for SSH output..."
	}

	return bodyStyle.Render(content)
}

func (m muxModel) viewStatus(width int) string {
	status := "ctrl+b n new | ctrl+b h/l switch | ctrl+b d detach"
	if m.prefix {
		status = "prefix active: n new | h/l switch | d detach"
	}

	if m.status != "" {
		status += " | " + m.status
	}

	return lipgloss.NewStyle().Width(width).Foreground(lipgloss.Color("245")).Render(status)
}

func (m muxModel) viewEnvironmentPicker(width, height int) string {
	pickerWidth := min(max(30, width-8), 100)

	box := lipgloss.NewStyle().
		Width(pickerWidth).
		Height(height).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1)
	if len(m.list.Items()) == 0 {
		return box.Render("No running Bastion environments are available. Press Esc to close.")
	}

	return box.Render(m.list.View())
}

func (m muxModel) currentSession() muxSession {
	if m.current < 0 || m.current >= len(m.sessions) {
		return muxSession{}
	}

	return m.sessions[m.current]
}

func (m *muxModel) upsertSession(session muxSession) {
	for index, existing := range m.sessions {
		if existing.EnvironmentID == session.EnvironmentID {
			m.sessions[index] = session
			m.current = index

			return
		}
	}

	m.sessions = append(m.sessions, session)
	m.current = len(m.sessions) - 1
}

func (m *muxModel) nextSession() {
	if len(m.sessions) == 0 {
		return
	}

	m.current = (m.current + 1) % len(m.sessions)
}

func (m *muxModel) previousSession() {
	if len(m.sessions) == 0 {
		return
	}

	m.current--
	if m.current < 0 {
		m.current = len(m.sessions) - 1
	}
}

func (m muxModel) commandContext() context.Context {
	if m.context == nil {
		return context.Background()
	}

	return m.context()
}

func (m muxModel) loadSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.backend.ListSessions(m.commandContext())
		return muxSessionsMsg{sessions: sessions, err: err}
	}
}

func (m muxModel) loadEnvironmentsCmd() tea.Cmd {
	return func() tea.Msg {
		var environments []environment.Environment

		cursor := ""
		for {
			page, err := m.environments.ListEnvironments(m.commandContext(), muxListLimit, cursor, nil)
			if err != nil {
				return muxEnvironmentsMsg{err: err}
			}

			environments = append(environments, page.Entries...)
			if page.Cursor == nil || *page.Cursor == "" {
				return muxEnvironmentsMsg{environments: environments}
			}

			cursor = *page.Cursor
		}
	}
}

func (m muxModel) openEnvironmentCmd(env environment.Environment) tea.Cmd {
	return func() tea.Msg {
		session, err := m.backend.OpenEnvironment(m.commandContext(), env)
		return muxStartedMsg{session: session, err: err}
	}
}

func (m muxModel) captureCurrentCmd() tea.Cmd {
	session := m.currentSession()
	if session.PaneID == "" {
		return nil
	}

	height := max(1, m.height-5)

	return func() tea.Msg {
		content, err := m.backend.Capture(m.commandContext(), session, height)
		return muxCaptureMsg{paneID: session.PaneID, content: content, err: err}
	}
}

func (m muxModel) resizeCurrentCmd() tea.Cmd {
	session := m.currentSession()
	if session.PaneID == "" {
		return nil
	}

	width := max(1, m.width-4)
	height := max(1, m.height-5)

	return func() tea.Msg {
		return muxSentKeyMsg{err: m.backend.Resize(m.commandContext(), session, width, height)}
	}
}

func (m muxModel) sendKeyPressCmd(msg tea.KeyPressMsg) tea.Cmd {
	session := m.currentSession()
	if session.PaneID == "" {
		return nil
	}

	text, keyName, ok := tmuxKeyFromBubbleTea(msg)
	if !ok {
		return nil
	}

	return func() tea.Msg {
		if text != "" {
			return muxSentKeyMsg{err: m.backend.SendText(m.commandContext(), session, text)}
		}

		return muxSentKeyMsg{err: m.backend.SendKey(m.commandContext(), session, keyName)}
	}
}

func (m muxModel) tickCmd() tea.Cmd {
	return tea.Tick(muxRefreshInterval, func(t time.Time) tea.Msg { return muxTickMsg(t) })
}

func tmuxKeyFromBubbleTea(msg tea.KeyPressMsg) (string, string, bool) {
	keyValue := msg.Key()
	if keyValue.Text != "" && msg.String() != "ctrl+b" {
		return keyValue.Text, "", true
	}

	value := msg.String()
	if keyName, ok := tmuxSpecialKeys[value]; ok {
		return "", keyName, true
	}

	if strings.HasPrefix(value, "ctrl+") && len(value) == len("ctrl+")+1 {
		return "", "C-" + strings.TrimPrefix(value, "ctrl+"), true
	}

	return "", "", false
}

var tmuxSpecialKeys = map[string]string{
	"enter":     "Enter",
	"tab":       "Tab",
	"backspace": "BSpace",
	"delete":    "Delete",
	"esc":       "Escape",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"home":      "Home",
	"end":       "End",
	"pgup":      "PageUp",
	"pgdown":    "PageDown",
}

type muxEnvironmentItem struct {
	environment.Environment
}

func (i muxEnvironmentItem) FilterValue() string {
	return muxEnvironmentLabel(i.Environment)
}

func (i muxEnvironmentItem) Title() string {
	return muxEnvironmentLabel(i.Environment)
}

func (i muxEnvironmentItem) Description() string {
	return i.ID + " | " + i.Status
}

func muxEnvironmentListItems(environments []environment.Environment) []list.Item {
	items := make([]list.Item, 0, len(environments))
	for _, env := range environments {
		items = append(items, muxEnvironmentItem{Environment: env})
	}

	return items
}

func muxSSHConnectableEnvironments(environments []environment.Environment) []environment.Environment {
	connectable := make([]environment.Environment, 0, len(environments))
	for _, env := range environments {
		if env.Status == ch.StateRunning || env.Status == ch.StatePaused {
			connectable = append(connectable, env)
		}
	}

	return connectable
}

func muxEnvironmentLabel(env environment.Environment) string {
	if env.Key != nil && *env.Key != "" {
		return *env.Key
	}

	return env.ID
}

func muxSessionLabel(session muxSession) string {
	if session.EnvironmentKey != "" {
		return session.EnvironmentKey
	}

	if session.WindowName != "" {
		return session.WindowName
	}

	return session.EnvironmentID
}

func muxTabZone(index int) string {
	return "mux-tab-" + strconv.Itoa(index)
}
