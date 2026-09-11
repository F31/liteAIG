// Package app composes the control and gateway planes into one process.
package app

import "fmt"

// Mode selects which planes are enabled in the liteaig process.
type Mode string

const (
	ModeAll     Mode = "all"
	ModeGateway Mode = "gateway"
	ModeControl Mode = "control"
)

// ParseMode validates a runtime mode.
func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	switch mode {
	case ModeAll, ModeGateway, ModeControl:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid runtime mode %q: expected all, gateway, or control", value)
	}
}

// Plan describes the in-process planes selected for a runtime mode.
type Plan struct {
	Mode    Mode
	Gateway bool
	Control bool
}

// Compose creates the process plan without introducing an internal network boundary.
func Compose(mode Mode) Plan {
	return Plan{
		Mode:    mode,
		Gateway: mode == ModeAll || mode == ModeGateway,
		Control: mode == ModeAll || mode == ModeControl,
	}
}
