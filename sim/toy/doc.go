// Package toy is the A0 acceptance protocol: a fixed-primary replicated
// register with no elections and no Raft. The primary replicates synchronously
// to all backups, waits for durability before acknowledging, and dedupes client
// requests by (client, seq). Under partitions it becomes unavailable, which is
// correct behavior the checkers must not mistake for a violation.
//
// Its purpose is calibration. Surviving 1k seeds proves the harness runs; it
// does not prove the harness catches anything. The mutants subpackage holds
// deliberately broken variants -- acknowledge before fsync, acknowledge before
// replicating, apply a retried request twice, iterate a map, read the wall
// clock, serve a stale read, restart from non-durable state -- each with a
// budget in seeds. A mutant that survives its budget means the harness is too
// weak, whatever the clean seeds say.
//
// The suite runs in CI permanently, recording kill-time per mutant, so a
// regression in harness sensitivity is visible before it costs us a bug.
//
// See DESIGN-A0 section 5 and DR-20. Lands in A0.9/A0.12.
package toy
