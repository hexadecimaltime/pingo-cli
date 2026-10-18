package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/hexadecimaltime/pingo-cli/history"
)

type model struct {
	state              appState
	sessionCode        string
	question           string
	sessionInput       textinput.Model
	renameInput        textinput.Model
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
		if !sessionCodePattern.MatchString(flags.sessionCode) {
			fmt.Println("Error: Session code must be exactly 6 digits.")
		}
		state = stateLoading
	}

	renameInput := textinput.New()
	renameInput.Placeholder = t(msgEnterSessionName)
	renameInput.CharLimit = 32
	renameInput.SetWidth(40)

	return model{
		state:         state,
		sessionCode:   flags.sessionCode,
		sessionInput:  sessionInput,
		renameInput:   renameInput,
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
	if m.state == stateConfigMenu {
		form, cmd := m.configForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.configForm = f
		}

		if m.configForm.State == huh.StateCompleted {
			_ = saveConfig(*m.cfg)
			_ = initI18n(m.cfg.Language)

			if m.cfg.HistoryEnabled {
				q, err := history.InitDB()
				if err == nil {
					m.store = history.NewSQLiteStore(q)
				}
			} else {
				m.store = &history.NoOpStore{}
			}

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
			if m.state != stateTextInput && m.state != stateMultiTextInput && m.state != stateSessionInput && m.state != stateRenameSession {
				return m, tea.Quit
			}
		case "ctrl+c", "esc", "ctrl+q":
			if msg.String() == "esc" && m.state == stateRenameSession {
				m.state = stateSessionInput
				return m, nil
			}
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
				case "r":
					if m.historyCursor >= 0 && m.historyCursor < len(m.sessionHistory) {
						m.state = stateRenameSession
						m.renameInput.Reset()
						if m.sessionHistory[m.historyCursor].Name.Valid {
							m.renameInput.SetValue(m.sessionHistory[m.historyCursor].Name.String)
						}
						m.renameInput.Focus()
						return m, textinput.Blink
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
					if !sessionCodePattern.MatchString(code) {
						m.errMsg = t(msgSessionCodeInvalid)
						return m, nil
					}
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
		case stateRenameSession:
			var cmd tea.Cmd
			m.renameInput.Focus()
			m.renameInput, cmd = m.renameInput.Update(msg)
			if msg.String() == "enter" {
				newName := strings.TrimSpace(m.renameInput.Value())
				if m.historyCursor >= 0 && m.historyCursor < len(m.sessionHistory) {
					code := m.sessionHistory[m.historyCursor].Code
					_ = m.store.UpdateSessionName(context.Background(), code, newName)
				}
				m.state = stateSessionInput
				return m, loadHistoryCmd(m.store)
			}
			return m, cmd
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
			b.WriteString(hintStyle.Render(t(msgRecentSessions)) + "\n")
			for i, s := range m.sessionHistory {
				cursorMark := " "
				if i == m.historyCursor {
					cursorMark = ">"
				}
				timeStr := s.LastJoinedAt.Format("02.01.2006 15:04")

				displayStr := s.Code
				if s.Name.Valid && s.Name.String != "" {
					displayStr = fmt.Sprintf("%s [%s]", s.Name.String, s.Code)
				}
				b.WriteString(fmt.Sprintf("%s %s (%s)\n", cursorMark, displayStr, timeStr))
			}
			historyView = "\n" + strings.TrimRight(b.String(), "\n")
		}

		return tea.NewView(joinLines(
			titleStyle.Render("PINGO"),
			"",
			m.sessionInput.View(),
			historyView,
			errLine(m.errMsg),
			"",
			footerLine(m),
		))

	case stateRenameSession:
		return tea.NewView(joinLines(
			titleStyle.Render(t(msgRenameSessionTitle)),
			"",
			hintStyle.Render(tData(msgNewNameForSession, map[string]any{"Code": m.sessionHistory[m.historyCursor].Code})),
			m.renameInput.View(),
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
		lines := []string{timeLeftLine(m.data)}
		if q != "" {
			lines = append(lines, q)
		}
		lines = append(lines, "", m.answerInput.View(), errLine(m.errMsg), "", footerLine(m))
		return tea.NewView(joinLines(lines...))

	case stateMultiTextInput:
		answers := strings.Join(m.answers, ", ")
		q := questionLine(m.question)
		lines := []string{timeLeftLine(m.data)}
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
		lines := []string{timeLeftLine(m.data)}
		if q != "" {
			lines = append(lines, q)
		}
		lines = append(lines, "", choiceListView(m.options, m.cursor, nil), "", footerLine(m))
		return tea.NewView(joinLines(lines...))

	case stateMultiChoice:
		q := questionLine(m.question)
		lines := []string{timeLeftLine(m.data)}
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
		lines := []string{timeLeftLine(m.data)}
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
	return questionStyle.Render(t(msgQuestionPrefix) + highlightMath(question))
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
		label := highlightMath(opt.label)
		if selected != nil {
			mark := " "
			if selected[i] {
				mark = "x"
			}
			fmt.Fprintf(&b, "%s [%s] %s\n", cursorMark, mark, label)
		} else {
			fmt.Fprintf(&b, "%s %s\n", cursorMark, label)
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
