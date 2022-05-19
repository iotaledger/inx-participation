package daemon

const (
	PriorityDisconnectINX = iota // no dependencies
	PriorityCloseParticipationDatabase
	PriorityStopParticipation
	PriorityStopParticipationAPI
)
