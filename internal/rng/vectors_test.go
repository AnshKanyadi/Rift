package rng

import (
	"encoding/json"
	"testing"
)

// These vectors pin this implementation. If a change to pcg.go, rand.go, or
// prf.go alters any value here, every bundle in seeds/ has silently stopped
// reproducing its recorded run -- so this test failing is not a nuisance, it is
// the alarm working.
//
// Provenance, stated plainly: these vectors are self-generated from this
// implementation. They prove that the generator has not changed since the day
// they were recorded. They are NOT evidence of bit-compatibility with
// O'Neill's reference PCG or with numpy's pcg64_dxsm; this package does not
// claim interoperability and nothing depends on it.
//
// Changing a value here is only ever correct alongside a deliberate,
// documented generator change that invalidates the corpus. That is a
// conversation, not a commit.

var pcgVectors = []struct {
	seed uint64
	want []uint64
}{
	{seed: 0, want: []uint64{0x8534ef6575032ad4, 0xa0536012a784ee61, 0x9453376cd6fd78e9, 0x7041e922873f57b7, 0x6238266310e6cd23, 0x24e507ff07ebe04c, 0xb4bd8dc8c8d00024, 0x9a886a0c7ca0cb30}},
	{seed: 1, want: []uint64{0x7f95d3cc2e9c13cc, 0xa0184b4f5ef467fa, 0xe798c6da5a492136, 0xe58cdb38885f936f, 0x9fa20720330a703f, 0x56e179fa92ad2176, 0x009a514feb5db43e, 0x7466993f6f038e84}},
	{seed: 42, want: []uint64{0x004f2d9b6aeb53f8, 0xf48147a34f2f4e7f, 0xb9e5b88ddefa4858, 0xceda53f56a5bdd1f, 0x21c4ef4febf1bef1, 0x74452e61ef8d4289, 0x42f7de1c386a349a, 0xa800d76f2ebb5dd3}},
	{seed: 1 << 63, want: []uint64{0x90e12d42522ce465, 0x65950f7bd77b8a2a, 0xe21c86d625ba88ef, 0xf81410601b9cd9fe, 0x79d1d590a1e8edf9, 0x1393ccf52ded438f, 0x336ab004949bbd4f, 0x75dffbf2258f99d7}},
	{seed: ^uint64(0), want: []uint64{0x59d689767c3a60af, 0xdba9109c1fbce6df, 0x22d4b7dcb4b5fddc, 0x89fab962977391b4, 0xb37532507db231c1, 0xe5a199330d50f036, 0xe3ecbf4359174f95, 0x2368267ac16ab607}},
}

func TestKnownAnswersPCG(t *testing.T) {
	for _, tc := range pcgVectors {
		r := New(tc.seed)
		for i, want := range tc.want {
			if got := r.Uint64(); got != want {
				t.Fatalf("New(%d) draw %d: got %#016x, want %#016x\n"+
					"THE GENERATOR HAS CHANGED. Every bundle in seeds/ has stopped "+
					"reproducing its recorded run.", tc.seed, i, got, want)
			}
		}
	}
}

var deriveVectors = []struct {
	name string
	key  string
	want []uint64
}{
	{name: "net.latency", key: "580f9b3731db4b22272280fccb8ab60c", want: []uint64{0x8b9d9b9ba1448384, 0x76abf05a72c58f7c, 0xf3b65c75b5a341b2, 0x74a96cfeb4301963}},
	{name: "fault.crash", key: "7ece3471eecc75dedce1b0e1c09f08be", want: []uint64{0xdc32c226f9374344, 0xe5339849572e1edc, 0xd2ffd5728aa6e94c, 0x11e541f23ab3d3ad}},
	{name: "workload", key: "2a492786f03162efcb024ad69c510f70", want: []uint64{0xb58e7a216979530d, 0x6d4545adaff63622, 0xfa01ca9057778489, 0x77a8ad36bb26ba8f}},
}

func TestKnownAnswersDerive(t *testing.T) {
	for _, tc := range deriveVectors {
		root := New(42)
		if got := root.DeriveKey(tc.name).String(); got != tc.key {
			t.Errorf("DeriveKey(%q): got %s, want %s", tc.name, got, tc.key)
		}
		d := root.Derive(tc.name)
		for i, want := range tc.want {
			if got := d.Uint64(); got != want {
				t.Fatalf("Derive(%q) draw %d: got %#016x, want %#016x", tc.name, i, got, want)
			}
		}
	}
}

var prfVectors = []struct {
	d       Domain
	a, b, c uint64
	want    uint64
}{
	{d: Domain(1), a: 0, b: 0, c: 0, want: 0x93ba1a4cde9cb857},
	{d: Domain(1), a: 1, b: 2, c: 3, want: 0xf3df82b228622c19},
	{d: Domain(2), a: 1, b: 2, c: 3, want: 0x359467ff1901d307},
	{d: Domain(7), a: ^uint64(0), b: 0, c: 1, want: 0x0d106d42c7bc0b00},
}

func TestKnownAnswersPRF(t *testing.T) {
	k, err := ParseKey("000102030405060708090a0b0c0d0e0f")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range prfVectors {
		if got := k.PRF(tc.d, tc.a, tc.b, tc.c); got != tc.want {
			t.Errorf("PRF(%d,%d,%d,%d): got %#016x, want %#016x\n"+
				"THE PRF HAS CHANGED. Every plan.json in seeds/ now executes a "+
				"different schedule than the one it recorded.",
				tc.d, tc.a, tc.b, tc.c, got, tc.want)
		}
	}
}

func TestKnownAnswersDerived(t *testing.T) {
	if got, want := New(7).Perm(10), []int{2, 3, 0, 9, 5, 8, 4, 1, 7, 6}; !equalInts(got, want) {
		t.Errorf("Perm(10) = %v, want %v", got, want)
	}
	r := New(7)
	want := []float64{0.6248256197582158, 0.8882669983164009, 0.2245666245042589}
	for i, w := range want {
		if got := r.Float64(); got != w {
			t.Errorf("Float64 draw %d = %v, want %v", i, got, w)
		}
	}
}

func TestKeyRoundTrip(t *testing.T) {
	// Plan files carry PRF keys, so this round trip is load-bearing for
	// reproducibility, not cosmetic.
	orig := New(1234).DeriveKey("net")

	parsed, err := ParseKey(orig.String())
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	if parsed != orig {
		t.Errorf("ParseKey(String()) = %v, want %v", parsed, orig)
	}

	type plan struct {
		Keys map[string]Key `json:"keys"`
	}
	blob, err := json.Marshal(plan{Keys: map[string]Key{"net": orig}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back plan
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.Keys["net"] != orig {
		t.Errorf("json round trip = %v, want %v (blob %s)", back.Keys["net"], orig, blob)
	}
}

func TestParseKeyRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "zz", "00", "000102030405060708090a0b0c0d0e", "000102030405060708090a0b0c0d0e0f00"} {
		if _, err := ParseKey(s); err == nil {
			t.Errorf("ParseKey(%q): want error, got nil", s)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
