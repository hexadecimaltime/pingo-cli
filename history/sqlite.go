package history

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed db/schema.sql
var schema string

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

	var userVersion int
	if err := db.QueryRow("PRAGMA user_version;").Scan(&userVersion); err != nil {
		return nil, err
	}

	if userVersion == 0 {
		db.Exec(schema)
		db.Exec("PRAGMA user_version = 1;")
		userVersion = 1
	}

	if userVersion == 1 {
		db.Exec("ALTER TABLE sessions ADD COLUMN name TEXT;")
		db.Exec("PRAGMA user_version = 2;")
		userVersion = 2
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

func (s *SQLiteStore) UpdateSessionName(ctx context.Context, code, name string) error {
	return s.q.UpdateSessionName(ctx, UpdateSessionNameParams{
		Name: sql.NullString{String: name, Valid: name != ""},
		Code: code,
	})
}
