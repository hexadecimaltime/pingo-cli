package history

import "context"

type Store interface {
	UpsertSession(ctx context.Context, code string) error
	SaveAnswer(ctx context.Context, sessionCode, question, answer string, options []string) error
	ListSessions(ctx context.Context) ([]Session, error)
}

type NoOpStore struct{}

func (n *NoOpStore) UpsertSession(ctx context.Context, code string) error { return nil }
func (n *NoOpStore) SaveAnswer(ctx context.Context, sessionCode, question, answer string, options []string) error {
	return nil
}
func (n *NoOpStore) ListSessions(ctx context.Context) ([]Session, error) { return []Session{}, nil }
