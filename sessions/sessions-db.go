package sessions

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/BalanceBalls/nekot/util"
)

type Session struct {
	ID               int
	Messages         []util.MessageToSend
	CreatedAt        string
	SessionName      string
	PromptTokens     int
	CompletionTokens int
	IsTemporary      bool
}

type SessionService struct {
	DB *sql.DB
}

func NewSessionService(db *sql.DB) *SessionService {
	return &SessionService{
		DB: db,
	}
}

func (ss *SessionService) GetMostRecessionSessionOrCreateOne() (Session, error) {
	var messages string
	session := Session{}

	row := ss.DB.QueryRow(`
SELECT sessions_id, sessions_messages, sessions_created_at, sessions_session_name FROM sessions ORDER BY sessions_created_at DESC LIMIT 1;
    `)
	err := row.Scan(&session.ID, &messages, &session.CreatedAt, &session.SessionName)
	// this is the case where we first boot up and we don't have any data at all
	// so we create a latest sesion
	if err != nil {
		if err == sql.ErrNoRows {
			return ss.InsertNewSession("default", []util.MessageToSend{}, false)
		} else {
			// An error occurred that isn't due to no rows being found
			return Session{}, err
		}
	}
	// If we reach this point, a session was found, so unmarshal the messages
	err = json.Unmarshal([]byte(messages), &session.Messages)
	if err != nil {
		return Session{}, err
	}
	// Return the found session
	return session, nil
}

func (ss *SessionService) GetSession(id int) (Session, error) {
	var messages string
	rows, err := ss.DB.Query(
		`SELECT 
			sessions_id,
			sessions_messages,
			sessions_created_at,
			sessions_session_name,
			prompt_tokens,
			completion_tokens,
			is_temporary
		FROM sessions 
		WHERE sessions_id=$1`,
		id,
	)
	if err != nil {
		// Return the error instead of panicking.
		return Session{}, err
	}
	// Ensure rows are closed after the function finishes.
	defer rows.Close()

	aSession := Session{}
	if rows.Next() {
		// Check for errors from Scan.
		if err := rows.Scan(
			&aSession.ID,
			&messages,
			&aSession.CreatedAt,
			&aSession.SessionName,
			&aSession.PromptTokens,
			&aSession.CompletionTokens,
			&aSession.IsTemporary); err != nil {
			return Session{}, err
		}
	} else {
		// If no rows were found, return a "not found" error.
		return Session{}, sql.ErrNoRows
	}
	// Check for any errors encountered during iteration.
	if err := rows.Err(); err != nil {
		return Session{}, err
	}

	err = json.Unmarshal([]byte(messages), &aSession.Messages)
	if err != nil {
		return Session{}, err
	}
	return aSession, nil
}

// get me all the sessions
func (ss *SessionService) GetAllSessions() ([]Session, error) {
	rows, err := ss.DB.Query(
		`SELECT 
			sessions_id,
			sessions_created_at,
			sessions_session_name,
			prompt_tokens,
			completion_tokens,
			is_temporary
		FROM sessions
		WHERE is_temporary = 0
		ORDER BY sessions_id DESC`,
	)
	if err != nil {
		return []Session{}, err
	}
	sessions := []Session{}
	for rows.Next() {
		aSession := Session{}
		rows.Scan(
			&aSession.ID,
			&aSession.CreatedAt,
			&aSession.SessionName,
			&aSession.PromptTokens,
			&aSession.CompletionTokens,
			&aSession.IsTemporary,
		)
		sessions = append(sessions, aSession)
	}
	defer rows.Close()

	return sessions, nil
}

func (ss *SessionService) UpdateSessionMessages(id int, messages []util.MessageToSend) error {
	jsonData, err := json.Marshal(messages)
	if err != nil {
		return err
	}

	_, err = ss.DB.Exec(`
			UPDATE sessions
			SET sessions_messages  = $1
			where sessions_id = $2
	`, jsonData, id)

	if err != nil {
		// TODO: handle better
		util.Log("I panic here")
		panic(err)
	}
	return nil
}

func (ss *SessionService) UpdateSessionTokens(id int, promptTokens, completionTokens int) error {
	_, err := ss.DB.Exec(`
			UPDATE sessions
			SET
				prompt_tokens = prompt_tokens + $1,
				completion_tokens = completion_tokens + $2
			WHERE sessions_id = $3
	`, promptTokens, completionTokens, id)

	if err != nil {
		// TODO: handle better
		util.Log("I panic here")
		panic(err)
	}
	return nil
}

func (ss *SessionService) UpdateSessionName(id int, name string) error {
	_, err := ss.DB.Exec(`
			UPDATE sessions
			SET sessions_session_name = $1
			where sessions_id= $2
	`, name, id)
	if err != nil {
		return err
	}

	return nil
}

func (ss *SessionService) InsertNewSession(name string, messages []util.MessageToSend, isTemporary bool) (Session, error) {
	// No session found, create a new one
	newSession := Session{
		// Initialize your session fields as needed
		// ID will be set by the database if using auto-increment
		IsTemporary: isTemporary,
		SessionName: name,                   // Set a default or generate a name
		Messages:    []util.MessageToSend{}, // Assuming Messages is a slice of Message
	}

	insertSQL := `INSERT INTO sessions (sessions_session_name, sessions_messages, is_temporary) VALUES (?, ?, ?);`
	messagesJSON, err := json.Marshal(newSession.Messages)
	if err != nil {
		return Session{}, err
	}
	result, err := ss.DB.Exec(
		insertSQL,
		newSession.SessionName,
		messagesJSON,
		isTemporary,
	)
	if err != nil {
		return Session{}, err
	}
	// Get the last inserted ID
	lastInsertID, err := result.LastInsertId()
	if err != nil {
		return Session{}, err
	}
	// Set the ID of the new session
	newSession.ID = int(lastInsertID)
	// Return the new session
	return newSession, nil
}

func (ss *SessionService) DeleteSession(id int) error {
	existing, err := ss.GetAllSessions()

	if err != nil {
		return err
	}

	if len(existing) == 1 {
		return errors.New("Cannot remove the only session")
	}

	_, err = ss.DB.Exec(`
		DELETE FROM sessions
		WHERE sessions_id = $1
	`, id)
	if err != nil {
		return err
	}

	return nil
}

func (ss *SessionService) SweepTemporarySessions() error {
	_, err := ss.DB.Exec(`
		DELETE FROM sessions
		WHERE is_temporary = 1
	`)
	if err != nil {
		return err
	}

	return nil
}

func (ss *SessionService) SaveQuickChat(id int) error {
	_, err := ss.DB.Exec(`
			UPDATE sessions
			SET is_temporary = 0
			where sessions_id= $1
	`, id)
	if err != nil {
		return err
	}

	return nil
}
