package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func InitDB() (*Queries, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(configDir, "pingo-cli", "history.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	schema, err := os.ReadFile("db/schema.sql")
	if err == nil {
		db.Exec(string(schema))
	}

	return New(db), nil
}

type SQLiteStore struct {
	q *Queries
}

func NewSQLiteStore(q *Queries) *SQLiteStore {
	return &SQLiteStore{q: q}
}

func (s *SQLiteStore) UpsertSession(ctx context.Context, code string) error {
	return s.q.UpsertSession(ctx, code)
}

func (s *SQLiteStore) SaveAnswer(ctx context.Context, sessionCode, question, answer string, options []string) error {
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return err
	}
	return s.q.SaveAnswer(ctx, SaveAnswerParams{
		SessionCode:  sessionCode,
		QuestionText: question,
		Options:      string(optionsJSON),
		GivenAnswer:  answer,
	})
}

func (s *SQLiteStore) ListSessions(ctx context.Context) ([]Session, error) {
	return s.q.ListSessions(ctx)
}

func ClearDB() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	dbPath := filepath.Join(configDir, "pingo-cli", "history.db")

	err = os.Remove(dbPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
