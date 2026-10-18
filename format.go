package main

import (
	"regexp"

	"charm.land/lipgloss/v2"
)

// Catppuccin Mocha Mauve for math highlighting
var mathStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cba6f7"))

var mathRegex = regexp.MustCompile(`(?s)(\$\$.*?\$\$|\$[^\$]+\$)`)

func highlightMath(text string) string {
	return mathRegex.ReplaceAllStringFunc(text, func(match string) string {
		return mathStyle.Render(match)
	})
}
