package monocore

// Outcome stands in for a closed enum: a fixed set of variants where a new one
// must break every consumer that has not decided what to do about it.
type Outcome uint8

const (
	OutcomeA Outcome = iota + 1
	OutcomeB
	OutcomeC
)
