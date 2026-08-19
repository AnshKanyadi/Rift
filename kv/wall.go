package kv

import "github.com/anshkanyadi/rift/clock"

// hlcWall converts a decoded uint64 back to a wall reading.
//
// It exists so the conversion happens in exactly one place. clock.Wall is a
// defined type precisely so that arbitrary int64s do not become wall readings by
// accident, and a codec is the one place that conversion is legitimate.
func hlcWall(v uint64) clock.Wall { return clock.NewWall(int64(v)) }
