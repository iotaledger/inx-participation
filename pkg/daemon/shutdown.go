package daemon

const (
	PriorityDisconnectINX = iota // no dependencies
	PriorityStopParticipation
	PriorityStopParticipationAPI
)
