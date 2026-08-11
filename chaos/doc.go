// Package chaos holds real-mode chaos runners: killing leaders on a cadence
// including mid-compaction, and injecting disk-full and torn writes through the
// production-adjacent Env.
//
// Lands in I2.
package chaos
