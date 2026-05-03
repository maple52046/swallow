package domain

import (
	"errors"
	"time"
)

type ServerStatus string

const (
	StatusUnknown ServerStatus = "unknown"
	StatusLive    ServerStatus = "live"
	StatusWarning ServerStatus = "warning"
	StatusError   ServerStatus = "error"
	StatusMaintain ServerStatus = "maintain"
	StatusOffline  ServerStatus = "offline"
)

type Server struct {
	ID        string
	Hostname  string
	IP        string
	Status    ServerStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

var ErrServerNotFound = errors.New("server not found")
var ErrHostnameTaken = errors.New("hostname already exists")
var ErrIPTaken = errors.New("ip already exists")
