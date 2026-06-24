-- name: UpsertSession :exec
INSERT INTO sessions (code, last_joined_at) 
VALUES (?, CURRENT_TIMESTAMP)
ON CONFLICT(code) DO UPDATE SET 
    last_joined_at = CURRENT_TIMESTAMP;

-- name: SaveAnswer :exec
INSERT INTO answers (session_code, question_text, options, given_answer) 
VALUES (?, ?, ?, ?);

-- name: ListSessions :many
SELECT * FROM sessions
ORDER BY last_joined_at DESC;