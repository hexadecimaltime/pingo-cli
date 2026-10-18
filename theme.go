package main

import (
	"regexp"

	"charm.land/lipgloss/v2"
	catppuccin "github.com/catppuccin/go"
)

var (
	countdownPattern   = regexp.MustCompile(`startCountdown\((\d+)\)`)
	numberPattern      = regexp.MustCompile(`^[+-]?\d+(?:\.\d+)?$`)
	surveyIDPattern    = regexp.MustCompile(`/surveys/([^/]+)/?`)
	sessionCodePattern = regexp.MustCompile(`^\d{6}$`)
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
