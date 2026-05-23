package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const (
	msgSessionCode            = "input.session_code"
	msgEnterYourAnswer        = "input.enter_answer"
	msgEnterNumber            = "input.enter_number"
	msgConnecting             = "status.connecting"
	msgWaitingActive          = "status.waiting_active"
	msgTimeRanOut             = "status.time_ran_out"
	msgSuccessSubmitted       = "status.success_submitted"
	msgFailedSubmitStatus     = "status.failed_submit_status"
	msgQuestionClosed         = "status.question_closed"
	msgSubmitting             = "status.submitting"
	msgNumberRequired         = "error.number_required"
	msgErrorPrefix            = "error.prefix"
	msgEnterToSubmit          = "hint.enter_submit"
	msgAnswersSoFar           = "hint.answers_so_far"
	msgEnterToAdd             = "hint.enter_add_empty_submit"
	msgSpaceToggleEnterSubmit = "hint.space_toggle_enter_submit"
	msgTimeLeft               = "label.time_left"
	msgQuestionPrefix         = "label.question_prefix"
	msgQuit                   = "label.quit"
	msgMove                   = "label.move"
	msgToggle                 = "label.toggle"
	msgSubmit                 = "label.submit"
	msgOptionDefault          = "label.option_default"
)

//go:embed locales/*.json
var localeFS embed.FS

var i18nLocalizer *i18n.Localizer

func initI18n(langFlag string) error {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	if _, err := bundle.LoadMessageFileFS(localeFS, "locales/en.json"); err != nil {
		return fmt.Errorf("load locales/en.json: %w", err)
	}
	if _, err := bundle.LoadMessageFileFS(localeFS, "locales/de.json"); err != nil {
		return fmt.Errorf("load locales/de.json: %w", err)
	}

	matcher := language.NewMatcher([]language.Tag{language.English, language.German})
	resolved := resolveLanguageTag(langFlag)
	matched, _, _ := matcher.Match(resolved)
	i18nLocalizer = i18n.NewLocalizer(bundle, matched.String(), language.English.String())

	return nil
}

func resolveLanguageTag(langFlag string) language.Tag {
	if tag, ok := parseLanguageTag(langFlag); ok {
		return tag
	}
	if tag, ok := detectEnvLanguage(); ok {
		return tag
	}
	return language.English
}

func detectEnvLanguage() (language.Tag, bool) {
	vars := []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"}
	for _, name := range vars {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		if tag, ok := parseLanguageTag(value); ok {
			return tag, true
		}
	}
	return language.English, false
}

func parseLanguageTag(raw string) (language.Tag, bool) {
	normalized := normalizeLanguageTag(raw)
	if normalized == "" {
		return language.English, false
	}
	tag, err := language.Parse(normalized)
	if err != nil {
		return language.English, false
	}
	return tag, true
}

func normalizeLanguageTag(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) > 0 {
		trimmed = parts[0]
	}
	if dot := strings.Index(trimmed, "."); dot >= 0 {
		trimmed = trimmed[:dot]
	}
	return strings.ReplaceAll(trimmed, "_", "-")
}

func t(id string) string {
	return tData(id, nil)
}

func tData(id string, data map[string]any) string {
	if i18nLocalizer == nil {
		return id
	}
	msg, err := i18nLocalizer.Localize(&i18n.LocalizeConfig{
		MessageID:    id,
		TemplateData: data,
	})
	if err != nil {
		return id
	}
	return msg
}
