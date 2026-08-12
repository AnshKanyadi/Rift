package clock

// Expected values. Nothing in this file constructs an input, and nothing in it
// speaks the clock package's authoring DSL -- so a change to how a schedule is
// spelled cannot touch a single line here. That is the point of the file
// existing: the rule "a vector file may change in its input construction but
// never in a want block" becomes a property of the directory layout rather than
// a thing a reviewer has to check.
//
// These vectors are self-generated and prove only that the schedule has not
// moved since the day they were recorded. Changing a value here is correct only
// alongside a deliberate, documented clock change that invalidates the corpus.
// That is a conversation, not a commit.

type tickVector struct {
	at      Instant
	ordinal int64
	mono    Mono
}

var tickWant = map[string][]tickVector{
	"drift": {
		{at: 9_998_001, ordinal: 1, mono: 10_000_000},
		{at: 19_996_001, ordinal: 2, mono: 20_000_000},
		{at: 29_994_002, ordinal: 3, mono: 30_000_000},
		{at: 39_992_002, ordinal: 4, mono: 40_000_000},
		{at: 49_990_002, ordinal: 5, mono: 50_000_000},
	},
	"hold": {
		{at: 500_000_000, ordinal: 1, mono: 500_000_000},
		{at: 1_000_000_000, ordinal: 2, mono: 1_000_000_000},
		{at: 1_401_606_426, ordinal: 3, mono: 1_500_000_000},
		{at: 1_803_212_852, ordinal: 4, mono: 2_000_000_000},
		{at: 2_204_819_278, ordinal: 5, mono: 2_500_000_001},
		{at: 2_606_425_703, ordinal: 6, mono: 3_000_000_000},
		{at: 3_010_000_000, ordinal: 7, mono: 3_500_000_000},
		{at: 3_510_000_000, ordinal: 8, mono: 4_000_000_000},
	},
	"reboot": {
		{at: 1_000_000_000, ordinal: 1, mono: 1_000_000_000},
		{at: 2_000_000_000, ordinal: 2, mono: 2_000_000_000},
		// The restart at 2.5s cancels the pending tick and the ordinal
		// starts again from one, because the monotonic epoch is per boot.
		{at: 3_500_000_000, ordinal: 1, mono: 1_000_000_000},
		{at: 4_500_000_000, ordinal: 2, mono: 2_000_000_000},
	},
}
