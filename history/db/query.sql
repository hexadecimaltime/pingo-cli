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

-- name: UpdateSessionName :exec
UPDATE sessions SET name = ? WHERE code = ?;

-- name: UpdateCorrectAnswer :exec
UPDATE answers 
SET correct_answer = ? 
WHERE id = (
    SELECT a.id FROM answers a
    WHERE a.session_code = ? 
    ORDER BY a.answered_at DESC 
    LIMIT 1
);