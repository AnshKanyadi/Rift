// Package soak is the continuous seed-hunting runner behind the CPU-hours
// claim. It runs mixed schedules across every phase's workloads on all cores
// and appends to SOAK.md: date, commit, seeds, operations, CPU-hours,
// violations, and inconclusive results.
//
// Inconclusive is tracked separately and is never counted as a pass. A rising
// inconclusive rate is a harness regression to be fixed by shrinking history
// windows or partitioning harder per key -- never by loosening a checker.
//
// See DESIGN-A0 DR-19. Lands in A0.11+.
package soak
