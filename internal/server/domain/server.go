package domain

import (
	"errors"
	"time"
)

type ServerStatus string

const (
	StatusUnknown  ServerStatus = "unknown"
	StatusLive     ServerStatus = "live"
	StatusWarning  ServerStatus = "warning"
	StatusError    ServerStatus = "error"
	StatusMaintain ServerStatus = "maintain"
	StatusOffline  ServerStatus = "offline"
)

// AgentStatus describes the connectivity state of an agent on this node.
type AgentStatus string

const (
	AgentStatusConnected    AgentStatus = "connected"
	AgentStatusDisconnected AgentStatus = "disconnected"
	AgentStatusStale        AgentStatus = "stale"
)

// OSInfo describes the operating system detected on the node.
type OSInfo struct {
	Type         string // e.g. "linux"
	Distribution string // e.g. "Ubuntu"
	Version      string // e.g. "22.04"
	KernelVersion string
	Architecture  string
}

// CPUInfo describes the CPU detected on the node.
type CPUInfo struct {
	Model      string
	Cores      int32
	Threads    int32
}

// MemoryInfo describes total RAM detected on the node.
type MemoryInfo struct {
	TotalKB int64
}

// GPUInfo describes a single GPU detected on the node.
type GPUInfo struct {
	Vendor string
	Model  string
	Index  int32
}

// NetworkInfo describes the node's network identity.
type NetworkInfo struct {
	Hostname   string
	PrimaryIP  string
	AllIPs     []string
}

// IPMIInfo describes the IPMI/BMC availability detected on the node.
type IPMIInfo struct {
	Available bool
	BMCIP     string
	Source    string // e.g. "ipmitool", "unknown"
	Status    string // e.g. "detected", "unavailable"
}

// Inventory holds the hardware and OS environment detected by the agent.
// All fields are optional; a nil Inventory means the agent has never reported.
type Inventory struct {
	OS        OSInfo
	CPU       CPUInfo
	Memory    MemoryInfo
	GPUs      []GPUInfo
	Network   NetworkInfo
	IPMI      IPMIInfo
	UpdatedAt time.Time
}

// AgentInfo holds agent connectivity metadata updated by the gRPC stream handler.
type AgentInfo struct {
	Status       AgentStatus
	LastSeenAt   time.Time
	AgentVersion string
}

// Server is the central domain entity representing a managed physical or virtual node.
type Server struct {
	ID        string
	Hostname  string
	IP        string
	Status    ServerStatus
	Inventory *Inventory // nil until agent first reports
	Agent     *AgentInfo // nil until agent first connects
	CreatedAt time.Time
	UpdatedAt time.Time
}

var ErrServerNotFound = errors.New("server not found")
var ErrHostnameTaken = errors.New("hostname already exists")
var ErrIPTaken = errors.New("ip already exists")
