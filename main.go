package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/PuerkitoBio/goquery"
	catppuccin "github.com/catppuccin/go"
	"github.com/hexadecimaltime/pingo-cli/history"
)

const baseURL = "https://pingo.coactum.de"

var version = "dev"

var (
	countdownPattern = regexp.MustCompile(`startCountdown\((\d+)\)`)
	numberPattern    = regexp.MustCompile(`^[+-]?\d+(?:\.\d+)?$`)
	surveyIDPattern  = regexp.MustCompile(`/surveys/([^/]+)/?`)
)

var (
	mocha = catppuccin.Mocha

	mochaText     = lipgloss.Color(mocha.Text().Hex)
	mochaSubtle   = lipgloss.Color(mocha.Subtext0().Hex)
	mochaSubtle2  = lipgloss.Color(mocha.Subtext1().Hex)
	mochaOverlay0 = lipgloss.Color(mocha.Overlay0().Hex)
	mochaMauve    = lipgloss.Color(mocha.Mauve().Hex)
	mochaRed      = lipgloss.Color(mocha.Red().Hex)
	mochaSurface  = lipgloss.Color(mocha.Surface0().Hex)
	mochaSurface1 = lipgloss.Color(mocha.Surface1().Hex)
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(mochaMauve)
	questionStyle = lipgloss.NewStyle().Bold(true).Foreground(mochaText)
	hintStyle     = lipgloss.NewStyle().Foreground(mochaSubtle)
	errStyle      = lipgloss.NewStyle().Foreground(mochaRed)
	okStyle       = lipgloss.NewStyle().Bold(true).Foreground(mochaMauve)
	footerStyle   = lipgloss.NewStyle().Background(mochaSurface).Foreground(mochaSubtle2)
	keyStyle      = lipgloss.NewStyle().Bold(true).Foreground(mochaMauve).Background(mochaSurface)
	barDimStyle   = lipgloss.NewStyle().Foreground(mochaSubtle2).Background(mochaSurface)
	barSepStyle   = lipgloss.NewStyle().Foreground(mochaSurface1).Background(mochaSurface)
	logStyle      = lipgloss.NewStyle().Foreground(mochaOverlay0)
)

type appState int

const (
	stateSessionInput appState = iota
	stateLoading
	stateTextInput
	stateMultiTextInput
	stateSingleChoice
	stateMultiChoice
	stateSubmitting
	stateDone
	stateError
	stateConfigMenu
)

type option struct {
	label string
	value string
}

type pollData struct {
	question      string
	formAction    string
	hidden        map[string]string
	inputName     string
	inputLabel    string
	surveyID      string
	isNumberInput bool
	isMultiText   bool
	isMultiChoice bool
	options       []option
	countdownEnd  time.Time
	hasCountdown  bool
}

type pollFoundMsg struct {
	data pollData
}

type pollErrorMsg struct {
	err error
}

type submitResultMsg struct {
	success bool
	status  int
	body    string
}

type tickMsg time.Time
type followUpMsg struct{}
type countdownUpdateMsg struct {
	end time.Time
}
type pollStoppedMsg struct{}
type fayeConnectedMsg struct {
	client *FayeClient
	events <-chan FayeEvent
}
type fayeEventMsg struct {
	event FayeEvent
}
type fayeErrorMsg struct {
	err error
}
type historyLoadedMsg []history.Session

// --- Configuration Logic ---

type appConfig struct {
	HistoryEnabled bool   `json:"history_enabled"`
	Language       string `json:"language"`
}

func getConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(configDir, "pingo-cli", "config.json")
}

func loadConfig() appConfig {
	var cfg appConfig
	path := getConfigPath()
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &cfg)
	}
	return cfg
}

func saveConfig(cfg appConfig) error {
	path := getConfigPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// --- CLI Flags Logic ---

type cliFlags struct {
	sessionCode  string
	langFlag     string
	historyTemp  bool
	historyOn    bool
	historyOff   bool
	clearHistory bool
	openConfig   bool
	showHelp     bool
	showVersion  bool
}

// --- Bubble Tea Model ---

type model struct {
	state              appState
	sessionCode        string
	question           string
	sessionInput       textinput.Model
	answerInput        textinput.Model
	options            []option
	cursor             int
	selected           map[int]bool
	answers            []string
	data               pollData
	spinner            spinner.Model
	errMsg             string
	statusMsg          string
	lastCountdownFetch time.Time
	wsActive           bool
	faye               *FayeClient
	fayeEvents         <-chan FayeEvent
	lastIteration      int
	lastTimestamp      time.Time
	subscribedSurvey   string
	width              int
	store              history.Store
	sessionHistory     []history.Session
	historyCursor      int
	cfg                *appConfig
	configForm         *huh.Form
}

func initialModel(flags cliFlags, cfg *appConfig, store history.Store) model {
	sessionInput := textinput.New()
	sessionInput.Placeholder = t(msgSessionCode)
	sessionInput.CharLimit = 64
	sessionInput.SetWidth(40)
	sessionInput.Focus()

	answerInput := textinput.New()
	answerInput.Placeholder = t(msgEnterYourAnswer)
	answerInput.CharLimit = 512
	answerInput.SetWidth(60)

	spin := spinner.New()
	spin.Spinner = spinner.Line

	state := stateSessionInput
	var configForm *huh.Form

	// Boot directly to config UI if the flag is provided
	if flags.openConfig {
		state = stateConfigMenu
		configForm = huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Enable Local History Tracking?").
					Description("Saves session codes and answers to your local disk.").
					Value(&cfg.HistoryEnabled),
				huh.NewSelect[string]().
					Title("Interface Language").
					Description("Sets the default language for the application.").
					Options(
						huh.NewOption("System Default", ""),
						huh.NewOption("English", "en"),
						huh.NewOption("Deutsch", "de"),
					).
					Value(&cfg.Language),
			),
		).WithShowHelp(true)
	} else if flags.sessionCode != "" {
		state = stateLoading
	}

	return model{
		state:         state,
		sessionCode:   flags.sessionCode,
		sessionInput:  sessionInput,
		answerInput:   answerInput,
		selected:      map[int]bool{},
		spinner:       spin,
		store:         store,
		historyCursor: -1,
		cfg:           cfg,
		configForm:    configForm,
	}
}

func loadHistoryCmd(store history.Store) tea.Cmd {
	return func() tea.Msg {
		sessions, err := store.ListSessions(context.Background())
		if err != nil {
			return nil
		}
		return historyLoadedMsg(sessions)
	}
}

func (m model) Init() tea.Cmd {
	if m.state == stateConfigMenu {
		return m.configForm.Init()
	}
	if m.state == stateLoading {
		return tea.Batch(m.spinner.Tick, fetchActivePoll(m.sessionCode), startFaye(m.sessionCode), tick(), loadHistoryCmd(m.store))
	}

	return tea.Batch(m.spinner.Tick, textinput.Blink, tick(), loadHistoryCmd(m.store))
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func afterDelay(msg tea.Msg, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return msg
	})
}

func startFaye(sessionCode string) tea.Cmd {
	if sessionCode == "" {
		return nil
	}

	return func() tea.Msg {
		client := NewFayeClient()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sessionChannel := "/sess/" + sessionCode
		if err := client.Connect(ctx, "wss://pingo.coactum.de/push/faye", sessionChannel); err != nil {
			return fayeErrorMsg{err: err}
		}

		return fayeConnectedMsg{client: client, events: client.Events()}
	}
}

func listenFaye(events <-chan FayeEvent) tea.Cmd {
	if events == nil {
		return nil
	}

	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return fayeErrorMsg{err: fmt.Errorf("websocket closed")}
		}

		return fayeEventMsg{event: event}
	}
}

func submitAndSave(m model, answers []string) tea.Cmd {
	saveCmd := func() tea.Msg {
		var opts []string
		for _, opt := range m.options {
			opts = append(opts, opt.label)
		}

		var humanReadableAnswers []string
		for _, ans := range answers {
			labelFound := ans
			for _, opt := range m.options {
				if opt.value == ans {
					labelFound = opt.label
					break
				}
			}
			humanReadableAnswers = append(humanReadableAnswers, labelFound)
		}

		givenAnswer := strings.Join(humanReadableAnswers, ", ")
		_ = m.store.SaveAnswer(context.Background(), m.sessionCode, m.question, givenAnswer, opts)
		return nil
	}
	return tea.Batch(submitVote(m.data, answers), saveCmd)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Delegate early to the huh form if in the config menu
	if m.state == stateConfigMenu {
		form, cmd := m.configForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.configForm = f
		}

		if m.configForm.State == huh.StateCompleted {
			// Save the mutated config back to disk
			_ = saveConfig(*m.cfg)
			// Apply the new language setting to the i18n module
			_ = initI18n(m.cfg.Language)

			// Reinitialize the store interface in case they toggled history
			if m.cfg.HistoryEnabled {
				q, err := history.InitDB()
				if err == nil {
					m.store = history.NewSQLiteStore(q)
				}
			} else {
				m.store = &history.NoOpStore{}
			}

			// Transition back to the session input state
			m.state = stateSessionInput
			return m, loadHistoryCmd(m.store)
		}

		if m.configForm.State == huh.StateAborted {
			return m, tea.Quit
		}

		return m, cmd
	}

	switch msg := msg.(type) {
	case historyLoadedMsg:
		m.sessionHistory = msg
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if m.data.hasCountdown {
			remaining := time.Until(m.data.countdownEnd)
			if remaining <= 0 && (m.state == stateTextInput || m.state == stateMultiTextInput || m.state == stateSingleChoice || m.state == stateMultiChoice) {
				m.state = stateDone
				m.statusMsg = t(msgTimeRanOut)

				return m, tea.Quit
			}
		}
		if shouldRefreshCountdown(m) && time.Since(m.lastCountdownFetch) >= time.Second {
			m.lastCountdownFetch = time.Now()
			cmds = append(cmds, fetchCountdown(m.sessionCode))
		}

		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)

		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width

		return m, nil

	case pollFoundMsg:
		m.data = msg.data
		m.question = msg.data.question
		m.options = msg.data.options
		m.answers = nil
		m.cursor = 0
		m.selected = map[int]bool{}
		m.answerInput.SetValue("")
		if msg.data.inputLabel != "" {
			m.answerInput.Placeholder = msg.data.inputLabel
		}
		if msg.data.surveyID != "" {
			m.subscribedSurvey = msg.data.surveyID
			_ = subscribeFayeSurvey(m.faye, msg.data.surveyID)
		}
		m.lastCountdownFetch = time.Now()
		m.sessionInput.Blur()

		saveSessionCmd := func() tea.Msg {
			_ = m.store.UpsertSession(context.Background(), m.sessionCode)
			return nil
		}

		if msg.data.isMultiChoice {
			m.answerInput.Blur()
			m.state = stateMultiChoice

			return m, saveSessionCmd
		}
		if len(msg.data.options) > 0 {
			m.answerInput.Blur()
			m.state = stateSingleChoice

			return m, saveSessionCmd
		}
		if msg.data.isMultiText {
			m.answerInput.Focus()
			m.state = stateMultiTextInput

			return m, saveSessionCmd
		}
		m.answerInput.Focus()
		m.state = stateTextInput

		return m, saveSessionCmd

	case pollErrorMsg:
		m.state = stateError
		m.errMsg = msg.err.Error()

		return m, nil

	case submitResultMsg:
		if msg.success {
			m.state = stateDone
			m.statusMsg = t(msgSuccessSubmitted)

			return m, afterDelay(followUpMsg{}, 3*time.Second)
		}
		m.state = stateError
		m.errMsg = tData(msgFailedSubmitStatus, map[string]any{"Status": msg.status})

		return m, tea.Quit

	case pollStoppedMsg:
		m.state = stateDone
		m.statusMsg = t(msgQuestionClosed)

		return m, afterDelay(followUpMsg{}, time.Second)

	case fayeConnectedMsg:
		m.faye = msg.client
		m.fayeEvents = msg.events
		m.wsActive = true

		return m, listenFaye(m.fayeEvents)

	case fayeEventMsg:
		m = handleFayeEvent(m, msg.event)

		return m, listenFaye(m.fayeEvents)

	case fayeErrorMsg:
		m.wsActive = false

		return m, nil

	case countdownUpdateMsg:
		m.data.countdownEnd = msg.end
		m.data.hasCountdown = true

		return m, nil

	case followUpMsg:
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			if m.state != stateTextInput && m.state != stateMultiTextInput && m.state != stateSessionInput {
				return m, tea.Quit
			}
		case "ctrl+c", "esc", "ctrl+q":
			return m, tea.Quit
		}

		switch m.state {
		case stateSessionInput:
			if len(m.sessionHistory) > 0 {
				switch msg.String() {
				case "up", "k":
					if m.historyCursor > 0 {
						m.historyCursor--
					} else if m.historyCursor == -1 {
						m.historyCursor = len(m.sessionHistory) - 1
					}
					if m.historyCursor >= 0 && m.historyCursor < len(m.sessionHistory) {
						m.sessionInput.SetValue(m.sessionHistory[m.historyCursor].Code)
						m.sessionInput.SetCursor(len(m.sessionInput.Value()))
					}
					return m, nil
				case "down", "j":
					if m.historyCursor < len(m.sessionHistory)-1 {
						m.historyCursor++
					}
					if m.historyCursor >= 0 && m.historyCursor < len(m.sessionHistory) {
						m.sessionInput.SetValue(m.sessionHistory[m.historyCursor].Code)
						m.sessionInput.SetCursor(len(m.sessionInput.Value()))
					}
					return m, nil
				}
			}

			var cmd tea.Cmd
			m.sessionInput.Focus()
			m.sessionInput, cmd = m.sessionInput.Update(msg)
			if msg.String() == "enter" {
				code := strings.TrimSpace(m.sessionInput.Value())
				if code != "" {
					m.sessionCode = code
					m.state = stateLoading

					return m, tea.Batch(m.spinner.Tick, fetchActivePoll(m.sessionCode), startFaye(m.sessionCode))
				}
			}

			return m, cmd
		case stateTextInput:
			var cmd tea.Cmd
			m.answerInput.Focus()
			m.answerInput, cmd = m.answerInput.Update(msg)
			if msg.String() == "enter" {
				value := strings.TrimSpace(m.answerInput.Value())
				if m.data.isNumberInput && !numberPattern.MatchString(value) {
					m.errMsg = t(msgNumberRequired)

					return m, nil
				}
				m.errMsg = ""
				m.state = stateSubmitting

				return m, submitAndSave(m, []string{value})
			}

			return m, cmd
		case stateMultiTextInput:
			var cmd tea.Cmd
			m.answerInput.Focus()
			m.answerInput, cmd = m.answerInput.Update(msg)
			if msg.String() == "enter" {
				value := strings.TrimSpace(m.answerInput.Value())
				if value == "" {
					m.state = stateSubmitting

					return m, submitAndSave(m, m.answers)
				}
				m.answers = append(m.answers, value)
				m.answerInput.SetValue("")
			}

			return m, cmd
		case stateSingleChoice:
			return updateSingleChoice(m, msg)
		case stateMultiChoice:
			return updateMultiChoice(m, msg)
		}
	}

	return m, nil
}

func updateSingleChoice(m model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()
	switch key.Code {
	case tea.KeyUp, 'k':
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown, 'j':
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case tea.KeySpace, tea.KeyEnter:
		if len(m.options) == 0 {
			return m, nil
		}
		choice := m.options[m.cursor]
		m.state = stateSubmitting

		return m, submitAndSave(m, []string{choice.value})
	}

	return m, nil
}

func updateMultiChoice(m model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()
	switch key.Code {
	case tea.KeyUp, 'k':
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown, 'j':
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case tea.KeySpace:
		if len(m.options) == 0 {
			return m, nil
		}
		m.selected[m.cursor] = !m.selected[m.cursor]
	case tea.KeyEnter:
		var values []string
		for idx, opt := range m.options {
			if m.selected[idx] {
				values = append(values, opt.value)
			}
		}
		m.state = stateSubmitting

		return m, submitAndSave(m, values)
	}

	return m, nil
}

func shouldRefreshCountdown(m model) bool {
	return !m.wsActive && m.sessionCode != "" && (m.state == stateTextInput || m.state == stateMultiTextInput || m.state == stateSingleChoice || m.state == stateMultiChoice)
}

func subscribeFayeSurvey(client *FayeClient, surveyID string) error {
	if client == nil || surveyID == "" {
		return nil
	}

	return client.Subscribe(context.Background(), "/s/"+surveyID)
}

func handleFayeEvent(m model, event FayeEvent) model {
	data := event.Data
	switch data.Type {
	case "status_change":
		if !timestampOk(&m, data.Timestamp) {

			return m
		}
		if data.Session != "" && m.subscribedSurvey != data.Session {
			m.subscribedSurvey = data.Session
			_ = subscribeFayeSurvey(m.faye, data.Session)
		}
		switch payloadString(data.Payload) {
		case "stop_scheduled":
			if data.Time > 0 {
				m.data.countdownEnd = time.Now().Add(time.Duration(data.Time) * time.Millisecond)
				m.data.hasCountdown = true
			}
		case "stopped":
			m.state = stateDone
			m.statusMsg = t(msgQuestionClosed)
		}
	case "countdown":
		if !iterationOk(&m, data.Iteration) {

			return m
		}
		if payload := payloadFloat(data.Payload); payload > 0 {
			m.data.countdownEnd = time.Now().Add(time.Duration(payload) * time.Millisecond)
			m.data.hasCountdown = true
		}
	}

	return m
}

func timestampOk(m *model, ts string) bool {
	if ts == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return true
	}
	if m.lastTimestamp.IsZero() || parsed.After(m.lastTimestamp) {
		m.lastTimestamp = parsed

		return true
	}

	return false
}

func iterationOk(m *model, iteration int) bool {
	if iteration == 0 {
		return true
	}
	if iteration == 1 || iteration > m.lastIteration {
		m.lastIteration = iteration

		return true
	}

	return false
}

func payloadString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {

		return value
	}

	return ""
}

func payloadFloat(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err == nil {

		return value
	}

	return 0
}

func (m model) View() tea.View {
	switch m.state {
	case stateConfigMenu:
		return tea.NewView(joinLines(
			titleStyle.Render("PINGO CONFIGURATION"),
			"",
			m.configForm.View(),
		))

	case stateSessionInput:
		historyView := ""
		if len(m.sessionHistory) > 0 {
			var b strings.Builder
			b.WriteString(hintStyle.Render("Recent Sessions (Up/Down to select):") + "\n")
			for i, s := range m.sessionHistory {
				cursorMark := " "
				if i == m.historyCursor {
					cursorMark = ">"
				}
				timeStr := s.LastJoinedAt.Format("02.01.2006 15:04")
				b.WriteString(fmt.Sprintf("%s %s (%s)\n", cursorMark, s.Code, timeStr))
			}
			historyView = "\n" + strings.TrimRight(b.String(), "\n")
		}

		return tea.NewView(joinLines(
			titleStyle.Render("PINGO"),
			"",
			m.sessionInput.View(),
			historyView,
			"",
			footerLine(m),
		))

	case stateLoading:
		return tea.NewView(joinLines(
			logStyle.Render(t(msgConnecting)),
			logStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), t(msgWaitingActive))),
			"",
			footerLine(m),
		))

	case stateTextInput:
		q := questionLine(m.question)

		lines := []string{
			timeLeftLine(m.data),
		}

		if q != "" {
			lines = append(lines, q)
		}

		lines = append(lines,
			"",
			m.answerInput.View(),
			errLine(m.errMsg),
			"",
			footerLine(m),
		)

		return tea.NewView(joinLines(lines...))

	case stateMultiTextInput:
		answers := strings.Join(m.answers, ", ")

		q := questionLine(m.question)
		lines := []string{
			timeLeftLine(m.data),
		}

		if q != "" {
			lines = append(lines, q)
		}

		lines = append(lines,
			"",
			hintStyle.Render(tData(msgAnswersSoFar, map[string]any{"Answers": answers})),
			m.answerInput.View(),
			errLine(m.errMsg),
			hintStyle.Render(t(msgEnterToAdd)),
			"",
			footerLine(m),
		)

		return tea.NewView(joinLines(lines...))

	case stateSingleChoice:
		q := questionLine(m.question)

		lines := []string{
			timeLeftLine(m.data),
		}

		if q != "" {
			lines = append(lines, q)
		}

		lines = append(lines,
			"",
			choiceListView(m.options, m.cursor, nil),
			"",
			footerLine(m),
		)

		return tea.NewView(joinLines(lines...))

	case stateMultiChoice:
		q := questionLine(m.question)

		lines := []string{
			timeLeftLine(m.data),
		}

		if q != "" {
			lines = append(lines, q)
		}

		lines = append(lines,
			"",
			choiceListView(m.options, m.cursor, m.selected),
			hintStyle.Render(t(msgSpaceToggleEnterSubmit)),
			"",
			footerLine(m),
		)

		return tea.NewView(joinLines(lines...))

	case stateSubmitting:
		q := questionLine(m.question)

		lines := []string{
			timeLeftLine(m.data),
		}

		if q != "" {
			lines = append(lines, q)
		}

		lines = append(lines,
			"",
			hintStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), t(msgSubmitting))),
			"",
			footerLine(m),
		)

		return tea.NewView(joinLines(lines...))

	case stateDone:
		return tea.NewView(joinLines(okStyle.Render(m.statusMsg)))

	case stateError:
		return tea.NewView(joinLines(errStyle.Render(m.errMsg)))
	default:
		return tea.NewView("")
	}
}

func timeLeftLine(data pollData) string {
	if !data.hasCountdown {
		return ""
	}
	remaining := time.Until(data.countdownEnd)
	if remaining < 0 {
		remaining = 0
	}
	label := hintStyle.Render(t(msgTimeLeft))
	value := lipgloss.NewStyle().Bold(true).Foreground(mochaMauve).Render(formatDuration(remaining))
	return label + value
}

func questionLine(question string) string {
	if strings.TrimSpace(question) == "" {
		return ""
	}
	return questionStyle.Render(t(msgQuestionPrefix) + question)
}

func errLine(msg string) string {
	if msg == "" {
		return ""
	}
	return errStyle.Render(msg)
}

func choiceListView(options []option, cursor int, selected map[int]bool) string {
	var b strings.Builder
	for i, opt := range options {
		cursorMark := " "
		if i == cursor {
			cursorMark = ">"
		}
		if selected != nil {
			mark := " "
			if selected[i] {
				mark = "x"
			}
			fmt.Fprintf(&b, "%s [%s] %s\n", cursorMark, mark, opt.label)
		} else {
			fmt.Fprintf(&b, "%s %s\n", cursorMark, opt.label)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatDuration(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 0 {
		seconds = 0
	}
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, secs)
}

func joinLines(lines ...string) string {
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		filtered = append(filtered, line)
	}
	start := 0
	end := len(filtered)
	for start < end && strings.TrimSpace(filtered[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(filtered[end-1]) == "" {
		end--
	}
	return strings.Join(filtered[start:end], "\n") + "\n"
}

func footerLine(m model) string {
	if m.state == stateDone || m.state == stateError || m.state == stateConfigMenu {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	left := footerHotkeys(m)
	right := keyStyle.Render("Ctrl+C") + barDimStyle.Render(" "+t(msgQuit))
	separator := barSepStyle.Render("|")
	gap := barDimStyle.Render("  ")
	content := left + gap + separator + gap + right
	innerWidth := width - 2
	if innerWidth < 10 {
		innerWidth = width
	}
	contentWidth := lipgloss.Width(content)
	if contentWidth < innerWidth {
		content += barDimStyle.Render(strings.Repeat(" ", innerWidth-contentWidth))
	}
	return footerStyle.Width(width).Padding(0, 1).Render(content)
}

func footerHotkeys(m model) string {
	parts := []string{}
	if m.state == stateSingleChoice || m.state == stateMultiChoice {
		parts = append(parts, keyStyle.Render("Up/Down")+barDimStyle.Render(" "+t(msgMove)))
	}
	if m.state == stateMultiChoice {
		parts = append(parts, keyStyle.Render("Space")+barDimStyle.Render(" "+t(msgToggle)))
	}
	parts = append(parts, keyStyle.Render("Enter")+barDimStyle.Render(" "+t(msgSubmit)))
	joiner := barDimStyle.Render("  ") + barSepStyle.Render("|") + barDimStyle.Render("  ")
	return strings.Join(parts, joiner)
}

func submitVote(data pollData, answers []string) tea.Cmd {
	return func() tea.Msg {
		values := url.Values{}
		for key, val := range data.hidden {
			values.Add(key, val)
		}
		for _, answer := range answers {
			values.Add(data.inputName, answer)
		}
		values.Add("commit", "Vote!")
		if _, ok := data.hidden["utf8"]; !ok {
			values.Add("utf8", "✓")
		}

		submitURL := data.formAction
		if !strings.HasPrefix(submitURL, "http") {
			submitURL = baseURL + submitURL
		}

		client := &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.PostForm(submitURL, values)
		if err != nil {
			return submitResultMsg{success: false, status: 0, body: err.Error()}
		}
		defer resp.Body.Close()
		success := resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther || resp.StatusCode == http.StatusOK
		return submitResultMsg{success: success, status: resp.StatusCode}
	}
}

func fetchActivePoll(sessionCode string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 15 * time.Second}
		sessionURL := fmt.Sprintf("%s/%s", baseURL, sessionCode)

		for {
			resp, err := client.Get(sessionURL)
			if err != nil {
				time.Sleep(3 * time.Second)
				continue
			}
			bodyBytes, err := readAll(resp)
			resp.Body.Close()
			if err != nil {
				time.Sleep(3 * time.Second)
				continue
			}
			data, ok := parsePollHTML(bodyBytes)
			if ok {
				return pollFoundMsg{data: data}
			}

			surveyURL := findSurveyURL(bodyBytes)
			if surveyURL != "" {
				resp2, err := client.Get(surveyURL)
				if err == nil {
					body2, err := readAll(resp2)
					resp2.Body.Close()
					if err == nil {
						data, ok := parseSurveyHTML(body2)
						if ok {
							return pollFoundMsg{data: data}
						}
					}
				}
			}

			time.Sleep(3 * time.Second)
		}
	}
}

func fetchCountdown(sessionCode string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 10 * time.Second}
		sessionURL := fmt.Sprintf("%s/%s", baseURL, sessionCode)
		resp, err := client.Get(sessionURL)
		if err != nil {
			return nil
		}
		bodyBytes, err := readAll(resp)
		resp.Body.Close()
		if err != nil {
			return nil
		}

		if _, ok := parsePollHTML(bodyBytes); ok {
			if end, ok := parseCountdown(bodyBytes); ok {
				return countdownUpdateMsg{end: end}
			}
			return nil
		}

		surveyURL := findSurveyURL(bodyBytes)
		if surveyURL != "" {
			resp2, err := client.Get(surveyURL)
			if err == nil {
				body2, err := readAll(resp2)
				resp2.Body.Close()
				if err == nil {
					if _, ok := parseSurveyHTML(body2); ok {
						if end, ok := parseCountdown(body2); ok {
							return countdownUpdateMsg{end: end}
						}
						return nil
					}
				}
			}
		}

		return pollStoppedMsg{}
	}
}

func parsePollHTML(body []byte) (pollData, bool) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {

		return pollData{}, false
	}
	form := findPollForm(doc)
	if form == nil || form.Length() == 0 {

		return pollData{}, false
	}

	return parseForm(doc, form, body)
}

func parseSurveyHTML(body []byte) (pollData, bool) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {

		return pollData{}, false
	}
	form := findPollForm(doc)
	if form == nil || form.Length() == 0 {

		return pollData{}, false
	}

	return parseForm(doc, form, body)
}

func findPollForm(doc *goquery.Document) *goquery.Selection {
	var best *goquery.Selection
	forms := doc.Find("form")
	forms.EachWithBreak(func(_ int, s *goquery.Selection) bool {
		action, _ := s.Attr("action")
		if strings.Contains(action, "/vote") && hasPollInputs(s) {
			best = s

			return false
		}

		return true
	})
	if best != nil {

		return best
	}
	forms.EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if hasPollInputs(s) {
			best = s

			return false
		}

		return true
	})

	return best
}

func hasPollInputs(form *goquery.Selection) bool {
	textInputs := form.Find("input[type='text'], input[type='number'], textarea")
	if textInputs.Length() > 0 {

		return true
	}
	optionInputs := form.Find("input[name='option'], input[name='option[]'], input[name='options[]'], input[name='survey_answer[]']")

	return optionInputs.Length() > 0
}

func extractSurveyID(hidden map[string]string, formAction string) string {
	keys := []string{"survey", "survey_id", "surveyid", "surveyId"}
	for _, key := range keys {
		if value, ok := hidden[key]; ok {
			value = strings.TrimSpace(value)
			if value != "" {

				return value
			}
		}
	}
	if matches := surveyIDPattern.FindStringSubmatch(formAction); len(matches) > 1 {

		return matches[1]
	}

	return ""
}

func extractQuestion(doc *goquery.Document, form *goquery.Selection) string {
	questionSelectors := "h1, h2, h3, h4, .lead, .question-text, #question-text, [data-question-text]"
	if form != nil && form.Length() > 0 {
		if question := firstText(form.PrevAllFiltered(questionSelectors)); question != "" {

			return question
		}

		var nestedQuestion string
		form.PrevAll().EachWithBreak(func(_ int, s *goquery.Selection) bool {
			nestedQuestion = firstText(s.Find(questionSelectors))

			return nestedQuestion == ""
		})
		if nestedQuestion != "" {

			return nestedQuestion
		}

		if question := firstText(form.Parent().Find(questionSelectors)); question != "" {

			return question
		}
	}

	selectors := []string{
		"div.question-text",
		".question-text",
		"#question-text",
		"[data-question-text]",
		"h1.lead",
		"h2.lead",
		"h3.lead",
		"h4.lead",
		".lead",
	}
	for _, selector := range selectors {
		if question := firstText(doc.Find(selector)); question != "" {

			return question
		}
	}

	return ""
}

func firstText(selection *goquery.Selection) string {
	var text string
	selection.EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text = strings.Join(strings.Fields(s.Text()), " ")

		return text == ""
	})

	return text
}

func parseForm(doc *goquery.Document, form *goquery.Selection, body []byte) (pollData, bool) {
	question := extractQuestion(doc, form)
	formAction, _ := form.Attr("action")
	if formAction == "" {
		formAction = "/vote"
	}

	hidden := map[string]string{}
	form.Find("input[type='hidden']").Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		value, _ := s.Attr("value")
		if name != "" {
			hidden[name] = value
		}
	})

	surveyID := extractSurveyID(hidden, formAction)

	countdownEnd, hasCountdown := parseCountdown(body)

	textInput := form.Find("input[type='text'], input[type='number']").First()
	textArea := form.Find("textarea").First()
	if textInput.Length() > 0 || textArea.Length() > 0 {
		target := textInput
		if textArea.Length() > 0 {
			target = textArea
		}
		inputName, _ := target.Attr("name")
		inputType, _ := target.Attr("type")
		id, _ := target.Attr("id")
		label := strings.TrimSpace(doc.Find(fmt.Sprintf("label[for='%s']", id)).First().Text())
		pageText := strings.ToLower(doc.Text())
		isMultiText := strings.HasSuffix(inputName, "[]") || strings.Contains(pageText, "multiple answers") || strings.Contains(pageText, "mehrere antworten")
		isNumberInput := inputType == "number" || strings.Contains(pageText, "please enter the number without a thousands delimiter")
		if label == "" {
			label = t(msgEnterYourAnswer)
		}
		labelLower := strings.ToLower(label)
		if isNumberInput && strings.Contains(labelLower, "please enter the number") {
			label = t(msgEnterNumber)
		}
		if isMultiText && !strings.HasSuffix(inputName, "[]") {
			inputName += "[]"
		}

		return pollData{
			question:      question,
			formAction:    formAction,
			hidden:        hidden,
			inputName:     inputName,
			inputLabel:    label,
			surveyID:      surveyID,
			isNumberInput: isNumberInput,
			isMultiText:   isMultiText,
			hasCountdown:  hasCountdown,
			countdownEnd:  countdownEnd,
		}, true
	}

	optionInputs := form.Find("input[name='option'], input[name='option[]'], input[name='options[]'], input[name='survey_answer[]']")
	options := make([]option, 0)
	isMultiChoice := false
	inputName := "option"
	optionInputs.Each(func(i int, s *goquery.Selection) {
		if typ, _ := s.Attr("type"); typ == "hidden" {

			return
		}
		if typ, _ := s.Attr("type"); typ == "checkbox" {
			isMultiChoice = true
		}
		if name, ok := s.Attr("name"); ok {
			inputName = name
		}
		id, _ := s.Attr("id")
		label := strings.TrimSpace(doc.Find(fmt.Sprintf("label[for='%s']", id)).First().Text())
		value, _ := s.Attr("value")
		if label == "" {
			label = tData(msgOptionDefault, map[string]any{"Index": i + 1})
		}
		options = append(options, option{label: label, value: value})
	})

	if len(options) == 0 {

		return pollData{}, false
	}

	return pollData{
		question:      question,
		formAction:    formAction,
		hidden:        hidden,
		inputName:     inputName,
		surveyID:      surveyID,
		isMultiChoice: isMultiChoice,
		options:       options,
		hasCountdown:  hasCountdown,
		countdownEnd:  countdownEnd,
	}, true
}

func findSurveyURL(body []byte) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {

		return ""
	}
	var surveyURL string
	doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href, _ := s.Attr("href")
		text := strings.ToLower(strings.TrimSpace(s.Text()))
		hrefLower := strings.ToLower(href)
		if (strings.Contains(hrefLower, "survey") && strings.Contains(hrefLower, "participate")) ||
			(strings.Contains(text, "survey") && strings.Contains(text, "participate")) {
			surveyURL = href

			return false
		}

		return true
	})
	if surveyURL == "" {

		return ""
	}
	if strings.HasPrefix(surveyURL, "http") {

		return surveyURL
	}

	return baseURL + surveyURL
}

func parseCountdown(body []byte) (time.Time, bool) {
	matches := countdownPattern.FindSubmatch(body)
	if len(matches) < 2 {

		return time.Time{}, false
	}
	value := string(matches[1])
	parsed, err := time.ParseDuration(value + "s")
	if err != nil {

		return time.Time{}, false
	}
	if parsed > 1000*time.Second {
		ms, err := time.ParseDuration(value + "ms")
		if err != nil {

			return time.Time{}, false
		}

		return time.Now().Add(ms), true
	}

	return time.Now().Add(parsed), true
}

func readAll(resp *http.Response) ([]byte, error) {
	if resp == nil {

		return nil, fmt.Errorf("empty response")
	}
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	if err != nil {

		return nil, err
	}

	return buf.Bytes(), nil
}

func main() {
	flags, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if flags.historyOn {
		cfg := loadConfig()
		cfg.HistoryEnabled = true
		if err := saveConfig(cfg); err != nil {
			fmt.Println("Error saving config:", err)
			os.Exit(1)
		}
		fmt.Println("History tracking is now permanently ENABLED.")
		return
	}

	if flags.historyOff {
		cfg := loadConfig()
		cfg.HistoryEnabled = false
		if err := saveConfig(cfg); err != nil {
			fmt.Println("Error saving config:", err)
			os.Exit(1)
		}
		fmt.Println("History tracking is now permanently DISABLED.")
		return
	}

	if flags.clearHistory {
		if err := history.ClearDB(); err != nil {
			fmt.Printf("Failed to clear database: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("History database cleared successfully.")
		return
	}

	if flags.showHelp {
		fmt.Print(buildUsage())
		return
	}

	if flags.showVersion {
		fmt.Println(version)
		return
	}

	cfg := loadConfig()

	// Command line lang flag overrides saved config
	activeLang := cfg.Language
	if flags.langFlag != "" {
		activeLang = flags.langFlag
	}

	if err := initI18n(activeLang); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	enableHistory := cfg.HistoryEnabled || flags.historyTemp

	var store history.Store
	if enableHistory {
		q, err := history.InitDB()
		if err != nil {
			fmt.Printf("Failed to initialize history database: %v\n", err)
			os.Exit(1)
		}
		store = history.NewSQLiteStore(q)
	} else {
		store = &history.NoOpStore{}
	}

	p := tea.NewProgram(initialModel(flags, &cfg, store))
	if _, err := p.Run(); err != nil {
		fmt.Println(t(msgErrorPrefix), err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (cliFlags, error) {
	fs := flag.NewFlagSet("pingo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var langFlag string
	fs.StringVar(&langFlag, "lang", "", "Language tag (e.g., en, de). Defaults to system locale.")
	fs.StringVar(&langFlag, "l", "", "Language tag (e.g., en, de). Defaults to system locale.")

	flagArgs, positionals, flags, err := splitArgs(args)
	if err != nil {
		return flags, err
	}
	if flags.showHelp {
		return flags, nil
	}
	if err := fs.Parse(flagArgs); err != nil {
		return flags, err
	}
	if len(positionals) > 0 {
		flags.sessionCode = strings.TrimSpace(positionals[0])
	}
	flags.langFlag = langFlag
	return flags, nil
}

func splitArgs(args []string) ([]string, []string, cliFlags, error) {
	flags := cliFlags{}
	flagArgs := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	valueFlags := map[string]bool{"--lang": true, "-l": true}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if arg == "--config" {
			flags.openConfig = true
			continue
		}
		if arg == "--history-on" {
			flags.historyOn = true
			continue
		}
		if arg == "--history-off" {
			flags.historyOff = true
			continue
		}
		if arg == "-c" || arg == "--clear" {
			flags.clearHistory = true
			continue
		}
		if arg == "-H" || arg == "--history" {
			flags.historyTemp = true
			continue
		}
		if arg == "-h" || arg == "--help" {
			flags.showHelp = true
			continue
		}
		if arg == "-v" || arg == "--version" {
			flags.showVersion = true
			continue
		}
		if strings.HasPrefix(arg, "--lang=") || strings.HasPrefix(arg, "-l=") {
			flagArgs = append(flagArgs, arg)
			continue
		}
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			if valueFlags[arg] {
				if i+1 >= len(args) {
					return nil, nil, flags, fmt.Errorf("missing value for %s", arg)
				}
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		positionals = append(positionals, arg)
	}

	return flagArgs, positionals, flags, nil
}

func buildUsage() string {
	return strings.Join([]string{
		"Usage:",
		"  pingo [SESSION_CODE] [flags]",
		"",
		"Flags:",
		"  --config           Open the interactive configuration menu",
		"  --history-on       Enable history tracking permanently",
		"  --history-off      Disable history tracking permanently",
		"  -H, --history      Enable history tracking for this run only",
		"  -c, --clear        Clear the local history database and exit",
		"  -l, --lang <tag>   Language tag (e.g., en, de). Defaults to system locale.",
		"  -v, --version      Print version and exit.",
		"  -h, --help         Show this help.",
		"",
		"Examples:",
		"  pingo --config",
		"  pingo 620610 --lang de",
		"  pingo --lang en 620610",
		"  pingo -l de",
		"  pingo -H",
		"  pingo -c",
	}, "\n") + "\n"
}
