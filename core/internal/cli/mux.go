package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

const (
	defaultMuxSessionName     = "bastion-mux"
	muxPlaceholderWindowName  = "__bastion_mux"
	muxEnvironmentListLimit   = 100
	defaultMuxPollInterval    = 500 * time.Millisecond
	defaultMuxWidth           = 100
	defaultMuxHeight          = 28
	minimumMuxSidebarWidth    = 18
	defaultMuxSidebarWidth    = 24
	muxWindowOptionEnvID      = "@bastion-env-id"
	muxWindowOptionEnvKey     = "@bastion-env-key"
	muxPlaceholderShellScript = "printf 'bastion mux backend ready. Press n in bastion mux to open an SSH session.\\n'; while :; do sleep 3600; done"
)

var errTmuxRequired = errors.New("tmux is required for bastion mux; install tmux and try again")

type muxAPI interface {
	ListEnvironments(context.Context, int, string, []string) (services.Page[environment.Environment], error)
}

type muxBackend interface {
	Preflight(context.Context) error
	Ensure(context.Context) error
	Sessions(context.Context) ([]muxSession, error)
	CreateSession(context.Context, environment.Environment) (muxSession, error)
	Capture(context.Context, string, int) (string, error)
	SendInput(context.Context, string, muxInput) error
	Resize(context.Context, string, int, int) error
}

type muxOptions struct {
	backend      muxBackend
	runTUI       muxTUIRunner
	pollInterval time.Duration
}

type muxTUIRunner func(context.Context, muxModel, io.Reader, io.Writer) error

type muxConfig struct {
	sessionName  string
	apiURL       string
	executable   string
	pollInterval time.Duration
}

type muxSession struct {
	Target         string
	Index          int
	Name           string
	EnvironmentID  string
	EnvironmentKey string
}

type muxInput struct {
	Literal string
	Key     string
}

func newMuxCommand(opts *rootOptions) *cobra.Command {
	return newMuxCommandWithOptions(opts, muxOptions{})
}

func newMuxCommandWithOptions(opts *rootOptions, muxOpts muxOptions) *cobra.Command {
	var sessionName string

	cmd := &cobra.Command{
		Use:   "mux",
		Short: "Multiplex persistent Bastion SSH sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config := muxConfig{
				sessionName:  sessionName,
				apiURL:       opts.apiURL,
				pollInterval: defaultMuxPollInterval,
			}
			if muxOpts.pollInterval > 0 {
				config.pollInterval = muxOpts.pollInterval
			}

			backend := muxOpts.backend
			if backend == nil {
				executable, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolve bastion executable: %w", err)
				}

				config.executable = executable
				backend = newTmuxMuxBackend(config)
			}

			if err := backend.Preflight(cmd.Context()); err != nil {
				return err
			}

			if err := backend.Ensure(cmd.Context()); err != nil {
				return err
			}

			runner := muxOpts.runTUI
			if runner == nil {
				runner = runMuxTUI
			}

			model := newMuxModel(cmd.Context(), backend, apiClient(opts), config)

			return runner(cmd.Context(), model, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&sessionName, "session", defaultMuxSessionName, "tmux session name")

	return cmd
}

func runMuxTUI(_ context.Context, model muxModel, stdin io.Reader, stdout io.Writer) error {
	program := tea.NewProgram(model, tea.WithInput(stdin), tea.WithOutput(stdout), tea.WithAltScreen())
	_, err := program.Run()

	return err
}

func loadRunningMuxEnvironments(ctx context.Context, api muxAPI) ([]environment.Environment, error) {
	var (
		cursor  string
		running []environment.Environment
	)

	for {
		page, err := api.ListEnvironments(ctx, muxEnvironmentListLimit, cursor, nil)
		if err != nil {
			return nil, err
		}

		for _, entry := range page.Entries {
			if entry.Status == ch.StateRunning {
				running = append(running, entry)
			}
		}

		if page.Cursor == nil || *page.Cursor == "" {
			return running, nil
		}

		cursor = *page.Cursor
	}
}

type tmuxMuxBackend struct {
	config  muxConfig
	look    func(string) (string, error)
	command func(context.Context, string, ...string) *exec.Cmd
}

func newTmuxMuxBackend(config muxConfig) *tmuxMuxBackend {
	return &tmuxMuxBackend{
		config:  config,
		look:    exec.LookPath,
		command: exec.CommandContext,
	}
}

func (b *tmuxMuxBackend) Preflight(context.Context) error {
	if _, err := b.look("tmux"); err != nil {
		return errTmuxRequired
	}

	return nil
}

func (b *tmuxMuxBackend) Ensure(ctx context.Context) error {
	if b.hasSession(ctx) {
		return nil
	}

	_, err := b.run(ctx, "new-session", "-d", "-s", b.config.sessionName, "-n", muxPlaceholderWindowName, "-x", "120", "-y", "40", muxPlaceholderShellScript)

	return err
}

func (b *tmuxMuxBackend) Sessions(ctx context.Context) ([]muxSession, error) {
	format := strings.Join([]string{"#{window_id}", "#{window_index}", "#{window_name}", "#{" + muxWindowOptionEnvID + "}", "#{" + muxWindowOptionEnvKey + "}"}, "\t")

	output, err := b.run(ctx, "list-windows", "-t", b.config.sessionName, "-F", format)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	sessions := make([]muxSession, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 5 || parts[3] == "" {
			continue
		}

		index, _ := strconv.Atoi(parts[1])
		sessions = append(sessions, muxSession{
			Target:         parts[0],
			Index:          index,
			Name:           parts[2],
			EnvironmentID:  parts[3],
			EnvironmentKey: parts[4],
		})
	}

	return sessions, nil
}

func (b *tmuxMuxBackend) CreateSession(ctx context.Context, env environment.Environment) (muxSession, error) {
	if env.ID == "" {
		return muxSession{}, errors.New("environment id is required")
	}

	executable := b.config.executable
	if executable == "" {
		resolved, err := os.Executable()
		if err != nil {
			return muxSession{}, fmt.Errorf("resolve bastion executable: %w", err)
		}

		executable = resolved
	}

	environmentKey := ""
	if env.Key != nil {
		environmentKey = *env.Key
	}

	name := sanitizeTmuxWindowName(environmentLabel(env))
	shellCommand := strings.Join([]string{
		"exec",
		shellQuote(executable),
		"--api-url",
		shellQuote(b.config.apiURL),
		"ssh",
		cliIDFlag,
		shellQuote(env.ID),
	}, " ")

	output, err := b.run(ctx, "new-window", "-d", "-P", "-F", "#{window_id}", "-t", b.config.sessionName+":", "-n", name, shellCommand)
	if err != nil {
		return muxSession{}, err
	}

	target := strings.TrimSpace(string(output))
	if target == "" {
		return muxSession{}, errors.New("tmux did not return a window id")
	}

	if _, err := b.run(ctx, "set-window-option", "-t", target, muxWindowOptionEnvID, env.ID); err != nil {
		return muxSession{}, err
	}

	if _, err := b.run(ctx, "set-window-option", "-t", target, muxWindowOptionEnvKey, environmentKey); err != nil {
		return muxSession{}, err
	}

	return muxSession{Target: target, Name: name, EnvironmentID: env.ID, EnvironmentKey: environmentKey}, nil
}

func (b *tmuxMuxBackend) Capture(ctx context.Context, target string, height int) (string, error) {
	if target == "" {
		return "", nil
	}

	if height <= 0 {
		height = defaultMuxHeight
	}

	output, err := b.run(ctx, "capture-pane", "-p", "-J", "-t", target, "-S", "-"+strconv.Itoa(height), "-E", "-")
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func (b *tmuxMuxBackend) SendInput(ctx context.Context, target string, input muxInput) error {
	if target == "" {
		return nil
	}

	if input.Literal != "" {
		_, err := b.run(ctx, "send-keys", "-t", target, "-l", "--", input.Literal)

		return err
	}

	if input.Key != "" {
		_, err := b.run(ctx, "send-keys", "-t", target, "--", input.Key)

		return err
	}

	return nil
}

func (b *tmuxMuxBackend) Resize(ctx context.Context, target string, width, height int) error {
	if target == "" || width <= 0 || height <= 0 {
		return nil
	}

	_, err := b.run(ctx, "resize-window", "-t", target, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))

	return err
}

func (b *tmuxMuxBackend) hasSession(ctx context.Context) bool {
	cmd := b.command(ctx, "tmux", "has-session", "-t", b.config.sessionName)

	return cmd.Run() == nil
}

func (b *tmuxMuxBackend) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := b.command(ctx, "tmux", args...)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err == nil {
		return output, nil
	}

	message := strings.TrimSpace(stderr.String())
	if message != "" {
		return nil, fmt.Errorf("tmux %s failed: %s: %w", tmuxAction(args), message, err)
	}

	return nil, fmt.Errorf("tmux %s failed: %w", tmuxAction(args), err)
}

func tmuxAction(args []string) string {
	if len(args) == 0 {
		return "command"
	}

	return args[0]
}

type muxModel struct {
	backend             muxBackend
	api                 muxAPI
	config              muxConfig
	sessions            []muxSession
	environments        []environment.Environment
	selected            int
	picker              int
	showPicker          bool
	loadingEnvironments bool
	pane                string
	message             string
	width               int
	height              int
}

func newMuxModel(_ context.Context, backend muxBackend, api muxAPI, config muxConfig) muxModel {
	if config.pollInterval <= 0 {
		config.pollInterval = defaultMuxPollInterval
	}

	return muxModel{
		backend: backend,
		api:     api,
		config:  config,
		width:   defaultMuxWidth,
		height:  defaultMuxHeight,
	}
}

func (m muxModel) Init() tea.Cmd {
	return tea.Batch(m.loadSessionsCmd(), m.tickCmd())
}

func (m muxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		return m, tea.Batch(m.resizeCmd(), m.captureCmd())
	case muxSessionsMsg:
		m.sessions = []muxSession(msg)
		m.selected = clampIndex(m.selected, len(m.sessions))

		return m, m.captureCmd()
	case muxPaneMsg:
		if active, ok := m.activeSession(); ok && active.Target == msg.target {
			m.pane = msg.contents
		}

		return m, nil
	case muxEnvironmentsMsg:
		m.environments = []environment.Environment(msg)
		m.picker = clampIndex(m.picker, len(m.environments))
		m.loadingEnvironments = false
		m.message = ""

		return m, nil
	case muxSessionCreatedMsg:
		session := muxSession(msg)
		m.sessions, m.selected = upsertMuxSession(m.sessions, session)
		m.showPicker = false
		m.loadingEnvironments = false
		m.message = "created SSH session for " + session.EnvironmentID

		return m, m.captureCmd()
	case muxErrorMsg:
		m.loadingEnvironments = false
		m.message = msg.message

		return m, nil
	case muxTickMsg:
		return m, tea.Batch(m.captureCmd(), m.tickCmd())
	case tea.KeyMsg:
		if m.showPicker {
			return m.updatePicker(msg)
		}

		return m.updateMain(msg)
	default:
		return m, nil
	}
}

func (m muxModel) updateMain(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "ctrl+d", "q", "d":
		return m, tea.Quit
	case "n":
		m.showPicker = true
		m.loadingEnvironments = true
		m.environments = nil
		m.picker = 0
		m.message = ""

		return m, m.loadEnvironmentsCmd()
	case "up", "k":
		if len(m.sessions) > 0 && m.selected > 0 {
			m.selected--
			m.pane = ""
		}

		return m, tea.Batch(m.resizeCmd(), m.captureCmd())
	case "down", "j":
		if len(m.sessions) > 0 && m.selected < len(m.sessions)-1 {
			m.selected++
			m.pane = ""
		}

		return m, tea.Batch(m.resizeCmd(), m.captureCmd())
	}

	input, ok := muxInputFromKey(key)
	if !ok {
		return m, nil
	}

	session, ok := m.activeSession()
	if !ok {
		return m, nil
	}

	return m, m.sendInputCmd(session.Target, input)
}

func (m muxModel) updatePicker(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "ctrl+d":
		return m, tea.Quit
	case "esc":
		m.showPicker = false
		m.loadingEnvironments = false

		return m, nil
	case "up", "k":
		if m.picker > 0 {
			m.picker--
		}

		return m, nil
	case "down", "j":
		if m.picker < len(m.environments)-1 {
			m.picker++
		}

		return m, nil
	case "enter":
		if m.loadingEnvironments || len(m.environments) == 0 {
			return m, nil
		}

		return m, m.createSessionCmd(m.environments[m.picker])
	}

	return m, nil
}

func (m muxModel) View() string {
	width := m.width
	if width <= 0 {
		width = defaultMuxWidth
	}

	height := m.height
	if height <= 0 {
		height = defaultMuxHeight
	}

	if height < 6 {
		height = 6
	}

	sidebarWidth := muxSidebarWidth(width)

	mainWidth := max(width-sidebarWidth-3, 10)

	bodyHeight := height - 2
	left := m.sidebarLines(bodyHeight)
	right := m.mainLines(bodyHeight, mainWidth)

	lines := []string{fitLine(m.headerLine(width), width)}
	for index := range bodyHeight {
		lines = append(lines, fitLine(left[index], sidebarWidth)+" | "+fitLine(right[index], mainWidth))
	}

	lines = append(lines, fitLine(m.footerLine(), width))

	return strings.Join(lines, "\n")
}

func (m muxModel) headerLine(width int) string {
	header := "bastion mux"
	if m.config.sessionName == "" {
		return header
	}

	right := "attached: " + m.config.sessionName

	padding := max(width-len(header)-len(right), 1)

	return header + strings.Repeat(" ", padding) + right
}

func (m muxModel) footerLine() string {
	if m.showPicker {
		return "up/down move  enter select  esc close"
	}

	if len(m.sessions) == 0 {
		return "n new session  d detach  q quit TUI"
	}

	return "up/down switch  n new session  d detach  q quit TUI"
}

func (m muxModel) sidebarLines(height int) []string {
	lines := []string{"SSH Sessions", ""}
	if len(m.sessions) == 0 {
		lines = append(lines, "No sessions")
	} else {
		for index, session := range m.sessions {
			prefix := "  "
			if index == m.selected {
				prefix = "> "
			}

			label := sessionLabel(session)

			lines = append(lines, prefix+label)
			if session.EnvironmentID != "" && session.EnvironmentID != label {
				lines = append(lines, "  "+session.EnvironmentID)
			}
		}
	}

	return padLines(lines, height)
}

func (m muxModel) mainLines(height, width int) []string {
	if m.showPicker {
		return padLines(m.pickerLines(width), height)
	}

	if len(m.sessions) == 0 {
		return padLines([]string{"No SSH sessions yet.", "", "Press n to create one from a running environment."}, height)
	}

	session, _ := m.activeSession()

	lines := []string{sessionLabel(session), strings.Repeat("-", max(1, width))}
	if m.message != "" {
		lines = append(lines, m.message, "")
	}

	pane := strings.TrimRight(m.pane, "\n")
	if pane == "" {
		lines = append(lines, "Waiting for SSH output...")
	} else {
		lines = append(lines, strings.Split(pane, "\n")...)
	}

	return padLines(lines, height)
}

func (m muxModel) pickerLines(width int) []string {
	boxWidth := max(min(width, 48), 28)

	border := "+" + strings.Repeat("-", boxWidth-2) + "+"
	lines := []string{
		border,
		boxLine("New SSH Session", boxWidth),
		boxLine("Choose a running environment:", boxWidth),
		boxLine("", boxWidth),
	}

	switch {
	case m.loadingEnvironments:
		lines = append(lines, boxLine("Loading environments...", boxWidth))
	case len(m.environments) == 0:
		lines = append(lines, boxLine("No running environments found.", boxWidth))
	default:
		for index, env := range m.environments {
			prefix := "  "
			if index == m.picker {
				prefix = "> "
			}

			lines = append(lines, boxLine(prefix+environmentLabel(env)+"  "+env.ID, boxWidth))
		}
	}

	lines = append(lines, boxLine("", boxWidth), boxLine("enter create  esc cancel", boxWidth), border)

	return lines
}

func (m muxModel) activeSession() (muxSession, bool) {
	if len(m.sessions) == 0 || m.selected < 0 || m.selected >= len(m.sessions) {
		return muxSession{}, false
	}

	return m.sessions[m.selected], true
}

func (m muxModel) loadSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.backend.Sessions(context.Background())
		if err != nil {
			return muxErrorMsg{message: err.Error()}
		}

		return muxSessionsMsg(sessions)
	}
}

func (m muxModel) loadEnvironmentsCmd() tea.Cmd {
	return func() tea.Msg {
		environments, err := loadRunningMuxEnvironments(context.Background(), m.api)
		if err != nil {
			return muxErrorMsg{message: err.Error()}
		}

		return muxEnvironmentsMsg(environments)
	}
}

func (m muxModel) createSessionCmd(env environment.Environment) tea.Cmd {
	return func() tea.Msg {
		session, err := m.backend.CreateSession(context.Background(), env)
		if err != nil {
			return muxErrorMsg{message: err.Error()}
		}

		return muxSessionCreatedMsg(session)
	}
}

func (m muxModel) captureCmd() tea.Cmd {
	session, ok := m.activeSession()
	if !ok {
		return nil
	}

	height := m.height - 4
	if height <= 0 {
		height = defaultMuxHeight
	}

	return func() tea.Msg {
		contents, err := m.backend.Capture(context.Background(), session.Target, height)
		if err != nil {
			return muxErrorMsg{message: err.Error()}
		}

		return muxPaneMsg{target: session.Target, contents: contents}
	}
}

func (m muxModel) sendInputCmd(target string, input muxInput) tea.Cmd {
	return func() tea.Msg {
		if err := m.backend.SendInput(context.Background(), target, input); err != nil {
			return muxErrorMsg{message: err.Error()}
		}

		return nil
	}
}

func (m muxModel) resizeCmd() tea.Cmd {
	session, ok := m.activeSession()
	if !ok {
		return nil
	}

	width := m.width - muxSidebarWidth(m.width) - 3
	height := m.height - 2

	return func() tea.Msg {
		if err := m.backend.Resize(context.Background(), session.Target, width, height); err != nil {
			return muxErrorMsg{message: err.Error()}
		}

		return nil
	}
}

func (m muxModel) tickCmd() tea.Cmd {
	return tea.Tick(m.config.pollInterval, func(time.Time) tea.Msg { return muxTickMsg{} })
}

type (
	muxSessionsMsg       []muxSession
	muxEnvironmentsMsg   []environment.Environment
	muxSessionCreatedMsg muxSession
	muxTickMsg           struct{}

	muxPaneMsg struct {
		target   string
		contents string
	}

	muxErrorMsg struct {
		message string
	}
)

func muxInputFromKey(key tea.KeyMsg) (muxInput, bool) {
	if len(key.Runes) > 0 {
		return muxInput{Literal: string(key.Runes)}, true
	}

	switch value := key.String(); value {
	case " ", "space":
		return muxInput{Literal: " "}, true
	case "enter":
		return muxInput{Key: "Enter"}, true
	case "tab":
		return muxInput{Key: "Tab"}, true
	case "backspace", "ctrl+h":
		return muxInput{Key: "BSpace"}, true
	case "delete":
		return muxInput{Key: "Delete"}, true
	case "esc":
		return muxInput{Key: "Escape"}, true
	default:
		if strings.HasPrefix(value, "ctrl+") && len(value) == len("ctrl+x") {
			return muxInput{Key: "C-" + value[len(value)-1:]}, true
		}
	}

	return muxInput{}, false
}

func upsertMuxSession(sessions []muxSession, session muxSession) ([]muxSession, int) {
	for index, existing := range sessions {
		if existing.Target == session.Target || existing.EnvironmentID == session.EnvironmentID {
			sessions[index] = session

			return sessions, index
		}
	}

	return append(sessions, session), len(sessions)
}

func clampIndex(index, length int) int {
	if length <= 0 {
		return 0
	}

	if index < 0 {
		return 0
	}

	if index >= length {
		return length - 1
	}

	return index
}

func muxSidebarWidth(width int) int {
	if width <= 0 {
		return defaultMuxSidebarWidth
	}

	limit := max(width/3, minimumMuxSidebarWidth)

	if defaultMuxSidebarWidth > limit {
		return limit
	}

	return defaultMuxSidebarWidth
}

func environmentLabel(env environment.Environment) string {
	if env.Key != nil && *env.Key != "" {
		return *env.Key
	}

	return env.ID
}

func sessionLabel(session muxSession) string {
	if session.EnvironmentKey != "" {
		return session.EnvironmentKey
	}

	if session.EnvironmentID != "" {
		return session.EnvironmentID
	}

	return session.Name
}

func sanitizeTmuxWindowName(value string) string {
	var builder strings.Builder

	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		case builder.Len() > 0:
			builder.WriteByte('-')
		}

		if builder.Len() >= 32 {
			break
		}
	}

	name := strings.Trim(builder.String(), "-")
	if name == "" {
		return "env"
	}

	return name
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func boxLine(value string, width int) string {
	innerWidth := max(width-4, 0)

	return "| " + fitLine(value, innerWidth) + " |"
}

func padLines(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}

	out := make([]string, height)
	copy(out, lines)

	return out
}

func fitLine(value string, width int) string {
	if width <= 0 {
		return ""
	}

	value = strings.ReplaceAll(value, "\t", "    ")

	runes := []rune(value)
	if len(runes) > width {
		return string(runes[:width])
	}

	return value + strings.Repeat(" ", width-len(runes))
}
