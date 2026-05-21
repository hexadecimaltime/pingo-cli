package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/PuerkitoBio/goquery"
	catppuccin "github.com/catppuccin/go"
)

const baseURL = "https://pingo.coactum.de"

var (
	countdownPattern = regexp.MustCompile(`startCountdown\((\d+)\)`)
	numberPattern    = regexp.MustCompile(`^[+-]?\d+(?:\.\d+)?$`)
)

var (
	mocha = catppuccin.Mocha

	mochaText   = lipgloss.Color(mocha.Text().Hex)
	mochaSubtle = lipgloss.Color(mocha.Subtext0().Hex)
	mochaMauve  = lipgloss.Color(mocha.Mauve().Hex)
	mochaRed    = lipgloss.Color(mocha.Red().Hex)
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(mochaMauve)
	questionStyle = lipgloss.NewStyle().Bold(true).Foreground(mochaText)
	hintStyle     = lipgloss.NewStyle().Foreground(mochaSubtle)
	errStyle      = lipgloss.NewStyle().Foreground(mochaRed)
	okStyle       = lipgloss.NewStyle().Bold(true).Foreground(mochaMauve)
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
	debug              bool
	lastCountdownFetch time.Time
}

func initialModel(sessionCode string, debug bool) model {
	sessionInput := textinput.New()
	sessionInput.Placeholder = "Enter PINGO session code..."
	sessionInput.CharLimit = 64
	sessionInput.SetWidth(40)
	sessionInput.Focus()

	answerInput := textinput.New()
	answerInput.Placeholder = "Enter your answer"
	answerInput.CharLimit = 512
	answerInput.SetWidth(60)

	spin := spinner.New()
	spin.Spinner = spinner.Line

	state := stateSessionInput
	if sessionCode != "" {
		state = stateLoading
	}

	return model{
		state:        state,
		sessionCode:  sessionCode,
		sessionInput: sessionInput,
		answerInput:  answerInput,
		selected:     map[int]bool{},
		spinner:      spin,
		debug:        debug,
	}
}

func (m model) Init() tea.Cmd {
	if m.state == stateLoading {
		return tea.Batch(m.spinner.Tick, fetchActivePoll(m.sessionCode, m.debug), tick())
	}
	return tea.Batch(m.spinner.Tick, tick())
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

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if shouldRefreshCountdown(m) && time.Since(m.lastCountdownFetch) >= time.Second {
			m.lastCountdownFetch = time.Now()
			cmds = append(cmds, fetchCountdown(m.sessionCode, m.debug))
		}
		return m, tea.Batch(cmds...)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
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
		m.lastCountdownFetch = time.Now()
		m.sessionInput.Blur()
		if msg.data.isMultiChoice {
			m.answerInput.Blur()
			m.state = stateMultiChoice
			return m, nil
		}
		if len(msg.data.options) > 0 {
			m.answerInput.Blur()
			m.state = stateSingleChoice
			return m, nil
		}
		if msg.data.isMultiText {
			m.answerInput.Focus()
			m.state = stateMultiTextInput
			return m, nil
		}
		m.answerInput.Focus()
		m.state = stateTextInput
		return m, nil
	case pollErrorMsg:
		m.state = stateError
		m.errMsg = msg.err.Error()
		return m, nil
	case submitResultMsg:
		if msg.success {
			m.state = stateDone
			m.statusMsg = "Success! Answer submitted."
			return m, afterDelay(followUpMsg{}, 3*time.Second)
		}
		m.state = stateError
		m.errMsg = fmt.Sprintf("Failed to submit. Status: %d", msg.status)
		return m, tea.Quit
	case pollStoppedMsg:
		m.state = stateDone
		m.statusMsg = "Question closed."
		return m, afterDelay(followUpMsg{}, time.Second)
	case countdownUpdateMsg:
		m.data.countdownEnd = msg.end
		m.data.hasCountdown = true
		return m, nil
	case followUpMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			if m.state != stateTextInput && m.state != stateMultiTextInput {
				return m, tea.Quit
			}
		case "ctrl+c", "esc", "ctrl+q":
			return m, tea.Quit
		}

		switch m.state {
		case stateSessionInput:
			var cmd tea.Cmd
			m.sessionInput.Focus()
			m.sessionInput, cmd = m.sessionInput.Update(msg)
			if msg.String() == "enter" {
				code := strings.TrimSpace(m.sessionInput.Value())
				if code != "" {
					m.sessionCode = code
					m.state = stateLoading
					return m, tea.Batch(m.spinner.Tick, fetchActivePoll(m.sessionCode, m.debug))
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
					m.errMsg = "Please enter a number without thousands delimiters, for example 42.7."
					return m, nil
				}
				m.errMsg = ""
				m.state = stateSubmitting
				return m, submitVote(m.data, []string{value})
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
					return m, submitVote(m.data, m.answers)
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
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.options) == 0 {
			return m, nil
		}
		choice := m.options[m.cursor]
		m.state = stateSubmitting
		return m, submitVote(m.data, []string{choice.value})
	}
	return m, nil
}

func updateMultiChoice(m model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case " ":
		if len(m.options) == 0 {
			return m, nil
		}
		m.selected[m.cursor] = !m.selected[m.cursor]
	case "enter":
		var values []string
		for idx, opt := range m.options {
			if m.selected[idx] {
				values = append(values, opt.value)
			}
		}
		m.state = stateSubmitting
		return m, submitVote(m.data, values)
	}
	return m, nil
}

func shouldRefreshCountdown(m model) bool {
	return m.sessionCode != "" && (m.state == stateTextInput || m.state == stateMultiTextInput || m.state == stateSingleChoice || m.state == stateMultiChoice)
}

func (m model) View() tea.View {
	switch m.state {
	case stateSessionInput:
		return tea.NewView(joinLines(
			titleStyle.Render("PINGO"),
			"",
			m.sessionInput.View(),
			"",
			hintStyle.Render("Enter to submit"),
		))
	case stateLoading:
		return tea.NewView(joinLines(
			hintStyle.Render("Connecting to session..."),
			hintStyle.Render(fmt.Sprintf("%s Waiting for an active poll or survey...", m.spinner.View())),
		))
	case stateTextInput:
		return tea.NewView(joinLines(
			timeLeftLine(m.data),
			questionLine(m.question),
			"",
			m.answerInput.View(),
			errLine(m.errMsg),
			hintStyle.Render("Enter to submit"),
		))
	case stateMultiTextInput:
		answers := strings.Join(m.answers, ", ")
		return tea.NewView(joinLines(
			timeLeftLine(m.data),
			questionLine(m.question),
			"",
			hintStyle.Render("Answers so far: "+answers),
			m.answerInput.View(),
			errLine(m.errMsg),
			hintStyle.Render("Enter to add, empty to submit"),
		))
	case stateSingleChoice:
		return tea.NewView(joinLines(
			timeLeftLine(m.data),
			questionLine(m.question),
			"",
			choiceListView(m.options, m.cursor, nil),
			hintStyle.Render("Enter to submit"),
		))
	case stateMultiChoice:
		return tea.NewView(joinLines(
			timeLeftLine(m.data),
			questionLine(m.question),
			"",
			choiceListView(m.options, m.cursor, m.selected),
			hintStyle.Render("Space to toggle, Enter to submit"),
		))
	case stateSubmitting:
		return tea.NewView(joinLines(
			timeLeftLine(m.data),
			questionLine(m.question),
			"",
			hintStyle.Render(fmt.Sprintf("%s Submitting...", m.spinner.View())),
		))
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
	label := hintStyle.Render("Time left: ")
	value := lipgloss.NewStyle().Bold(true).Foreground(mochaMauve).Render(formatDuration(remaining))
	return label + value
}

func questionLine(question string) string {
	if strings.TrimSpace(question) == "" {
		return ""
	}
	return questionStyle.Render("Q: " + question)
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
		if strings.TrimSpace(line) == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n") + "\n"
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

func fetchActivePoll(sessionCode string, debug bool) tea.Cmd {
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
			if debug {
				debugInspectHTML(bodyBytes)
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
						if debug {
							debugInspectHTML(body2)
						}
					}
				}
			}

			time.Sleep(3 * time.Second)
		}
	}
}

func fetchCountdown(sessionCode string, debug bool) tea.Cmd {
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
		if debug {
			debugInspectHTML(bodyBytes)
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
					if debug {
						debugInspectHTML(body2)
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

func parseForm(doc *goquery.Document, form *goquery.Selection, body []byte) (pollData, bool) {
	question := strings.TrimSpace(doc.Find("div.question-text").First().Text())
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
		if label == "" {
			label = "Enter your answer"
		}
		pageText := strings.ToLower(doc.Text())
		isMultiText := strings.HasSuffix(inputName, "[]") || strings.Contains(pageText, "multiple answers") || strings.Contains(pageText, "mehrere antworten")
		isNumberInput := inputType == "number" || strings.Contains(pageText, "please enter the number without a thousands delimiter")
		if isMultiText && !strings.HasSuffix(inputName, "[]") {
			inputName += "[]"
		}

		return pollData{
			question:      question,
			formAction:    formAction,
			hidden:        hidden,
			inputName:     inputName,
			inputLabel:    label,
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
			label = fmt.Sprintf("Option %d", i+1)
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

func debugInspectHTML(body []byte) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "[debug] parse error:", err)
		return
	}
	forms := doc.Find("form")
	fmt.Fprintf(os.Stderr, "[debug] forms=%d\n", forms.Length())
	forms.Each(func(i int, s *goquery.Selection) {
		action, _ := s.Attr("action")
		textInputs := s.Find("input[type='text'], input[type='number'], textarea").Length()
		optionInputs := s.Find("input[name='option'], input[name='option[]'], input[name='options[]'], input[name='survey_answer[]']").Length()
		fmt.Fprintf(os.Stderr, "[debug] form[%d] action=%q textInputs=%d optionInputs=%d\n", i, action, textInputs, optionInputs)
	})
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
	// Heuristic: values over 1000 are likely milliseconds.
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
	sessionCode := ""
	debug := false
	for _, arg := range os.Args[1:] {
		if arg == "--debug" || arg == "-d" {
			debug = true
			continue
		}
		if sessionCode == "" {
			sessionCode = strings.TrimSpace(arg)
		}
	}

	p := tea.NewProgram(initialModel(sessionCode, debug))
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
