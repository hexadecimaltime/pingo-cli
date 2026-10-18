package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const baseURL = "https://pingo.coactum.de"

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

func subscribeFayeSurvey(client *FayeClient, surveyID string) error {
	if client == nil || surveyID == "" {
		return nil
	}

	return client.Subscribe(context.Background(), "/s/"+surveyID)
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
