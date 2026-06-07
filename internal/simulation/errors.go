package simulation

import "errors"

var (
	ErrSimNotFound        = errors.New("simulation not found")
	ErrSimAlreadyRunning  = errors.New("simulation is already running")
	ErrSimNotRunning      = errors.New("simulation is not running")
	ErrInvalidConfig      = errors.New("invalid simulation config")
	ErrTooFewPersonas     = errors.New("need at least 2 personas")
	ErrTooManyPersonas    = errors.New("maximum 5 personas allowed")
	ErrDuplicatePersonaID = errors.New("duplicate persona id")
	ErrEmptyTopic         = errors.New("topic is required")
	ErrSimCancelled       = errors.New("simulation was cancelled")
)
