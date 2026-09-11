// Package identity owns canonical principals and authentication records.
package identity

import "time"

type LocalAdmin struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	Status       string
	CreatedAt    time.Time
}
