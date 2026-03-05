package domain

import "time"

// Project is a logical grouping of events identified by a unique DSN key.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	DSNKey    string    `json:"dsn_key"`
	CreatedAt time.Time `json:"created_at"`
}
