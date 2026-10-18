package main

import (
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/hexadecimaltime/pingo-cli/history"
)

// --- Shared Types & Bubble Tea Messages ---

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
	stateRenameSession
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

// --- Program Entry Point ---

var version = "dev"

func main() {
	flags, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	cfg := loadConfig()

	activeLang := cfg.Language
	if flags.langFlag != "" {
		activeLang = flags.langFlag
	}

	if err := initI18n(activeLang); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if flags.historyOn {
		cfg.HistoryEnabled = true
		if err := saveConfig(cfg); err != nil {
			fmt.Println("Error saving config:", err)
			os.Exit(1)
		}
		fmt.Println("History tracking is now permanently ENABLED.")
		return
	}

	if flags.historyOff {
		cfg.HistoryEnabled = false
		if err := saveConfig(cfg); err != nil {
			fmt.Println("Error saving config:", err)
			os.Exit(1)
		}
		fmt.Println("History tracking is now permanently DISABLED.")
		return
	}

	if flags.clearHistory {
		var confirm bool
		err := huh.NewConfirm().
			Title(t(msgConfirmClearHistory)).
			Affirmative(t(msgConfirmYes)).
			Negative(t(msgConfirmNo)).
			Value(&confirm).
			Run()

		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}

		if !confirm {
			fmt.Println(t(msgOperationCancelled))
			return
		}

		if err := history.ClearDB(); err != nil {
			fmt.Println(tData(msgFailedClearDB, map[string]any{"Err": err.Error()}))
			os.Exit(1)
		}
		fmt.Println(t(msgHistoryCleared))
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
