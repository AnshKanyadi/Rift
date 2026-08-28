//go:build rift_cgo

package riftcgo

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE HEADER IS THE SOURCE OF TRUTH AND THIS TEST READS IT.
//
// Three declarations of one set exist: Status::Code in C++, rift_status in the
// C header, and codeError's switch in Go. The first pair is held together by
// -Werror=switch in ToC. NOTHING HELD THE THIRD, and Go has no exhaustiveness
// check to borrow -- a `switch` over an untyped constant set compiles happily
// while missing every case.
//
// kBusy proved all of this at B5.3: it was added to Status::Code, and both the
// boundary and the wrapper compiled without a word. The boundary now refuses;
// this is what makes the wrapper refuse.
//
//	AN ASSERTION ABOUT THE MEMBERS PRESENT CANNOT FAIL ON A MEMBER ADDED.
//	Only an assertion about the SET can, and only if it derives the set from
//	somewhere other than the code under test.
func TestEveryStatusTheHeaderDeclaresIsMapped(t *testing.T) {
	src, err := os.ReadFile("../../engine-cpp/capi/rift.h")
	if err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	block := regexp.MustCompile(`(?s)typedef enum \{(.*?)\} rift_status;`).FindSubmatch(src)
	if block == nil {
		t.Fatal("could not find the rift_status enum; this test is reading the wrong thing")
	}
	decl := regexp.MustCompile(`(RIFT_[A-Z_]+)\s*=\s*(\d+)`).FindAllStringSubmatch(string(block[1]), -1)
	if len(decl) < 10 {
		t.Fatalf("found only %d enumerators, which means the parse is wrong, not that the enum shrank", len(decl))
	}

	for _, d := range decl {
		name, _ := d[1], d[2]
		v, err := strconv.Atoi(d[2])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		err = errForCode(v)
		if name == "RIFT_OK" {
			if err != nil {
				t.Errorf("RIFT_OK mapped to an error: %v", err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s (=%d) mapped to nil, which reports a failure as a success", name, v)
			continue
		}
		if strings.Contains(err.Error(), "unknown status") {
			t.Errorf("%s (=%d) is declared in the header and unmapped in codeError: %v\n"+
				"Add a case. A code this build cannot name reaches every caller as an "+
				"opaque number, which is the failure this test exists for.", name, v, err)
		}
	}
}

// AND THE REVERSE DIRECTION, which is the one an exhaustiveness check usually
// forgets: a value the header does NOT declare must still be reported rather
// than mapped to whatever case happens to be nearby.
func TestAnUndeclaredStatusIsReportedAndNotGuessed(t *testing.T) {
	err := errForCode(4242)
	if err == nil {
		t.Fatal("an undeclared status mapped to success")
	}
	if !strings.Contains(err.Error(), "4242") {
		t.Fatalf("an undeclared status must name itself so it can be chased; got %v", err)
	}
}
