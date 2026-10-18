package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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
		if arg == "--clear" {
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
		"  --clear        Clear the local history database and exit",
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
	}, "\n") + "\n"
}
