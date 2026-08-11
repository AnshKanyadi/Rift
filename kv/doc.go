// Package kv implements MVCC and distributed transactions: versioned key
// encoding, data/lock/write records, prewrite, commit, resolve, parallel-commit
// staging records and their recovery protocol, and reads at a timestamp with
// uncertainty-interval handling.
//
// Lands in A5/A6/A9.
package kv
