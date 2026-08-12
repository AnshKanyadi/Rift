package plan_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/anshkanyadi/rift/internal/sorted"
	"github.com/anshkanyadi/rift/sim/plan"
)

// TestPlanCarriesNoFloatingPoint is the ruling made mechanical: the plan is
// replay identity, so no float crosses the serialization boundary in either
// direction.
//
// A fraction surviving into a serialized plan would be multiplied on the
// replaying machine, and `off + slope*(t-start)` is exactly the multiply-add an
// arm64 fuses into one FMA and an amd64 without FMA does not. That is the
// cross-architecture last-bit divergence the float rule exists to kill, and
// carrying it in the corpus would reconstruct it with an audience.
//
// Checked two ways, because either alone has a hole:
//
//   - **Structurally**, by walking the Plan type's whole reachable field graph.
//     A grep of one struct would miss a float added three types down.
//   - **By value**, by decoding a real plan with json.Number and requiring
//     every number to parse as an integer. The type walk cannot see a float
//     smuggled through `any`, and this can.
func TestPlanCarriesNoFloatingPoint(t *testing.T) {
	t.Run("type graph", func(t *testing.T) {
		var found []string
		walkType(reflect.TypeOf(plan.Plan{}), "Plan", make(map[reflect.Type]bool), &found)
		if len(found) != 0 {
			t.Errorf("the plan type graph reaches %d floating-point field(s):\n  %s",
				len(found), strings.Join(found, "\n  "))
		}
	})

	t.Run("serialized values", func(t *testing.T) {
		p, err := plan.Materialize(4242, plan.DefaultGenConfig())
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		b, err := plan.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.UseNumber()
		var tree any
		if err := dec.Decode(&tree); err != nil {
			t.Fatalf("decode: %v", err)
		}

		var bad []string
		walkValue(tree, "$", &bad)
		if len(bad) != 0 {
			t.Errorf("the serialized plan carries %d non-integer number(s):\n  %s",
				len(bad), strings.Join(bad, "\n  "))
		}
	})
}

// walkType reports every reachable field whose kind is a float. It follows
// structs, pointers, slices, arrays and maps, and remembers types it has seen
// so a recursive type does not loop.
func walkType(t reflect.Type, path string, seen map[reflect.Type]bool, found *[]string) {
	if t == nil || seen[t] {
		return
	}
	seen[t] = true

	switch t.Kind() {
	case reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		*found = append(*found, fmt.Sprintf("%s is %s", path, t.Kind()))
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			walkType(f.Type, path+"."+f.Name, seen, found)
		}
	case reflect.Ptr, reflect.Slice, reflect.Array:
		walkType(t.Elem(), path+"[]", seen, found)
	case reflect.Map:
		walkType(t.Key(), path+"{key}", seen, found)
		walkType(t.Elem(), path+"{}", seen, found)
	case reflect.Interface:
		// An `any` field would let a float through unseen by this walk. The
		// value-level half of the test is what covers that, and a plan that
		// grows one should be looked at hard.
		*found = append(*found, fmt.Sprintf("%s is an interface; a float could pass through it unchecked", path))
	}
}

// walkValue reports every JSON number that is not an integer.
func walkValue(v any, path string, bad *[]string) {
	switch v := v.(type) {
	case json.Number:
		if _, err := v.Int64(); err != nil {
			*bad = append(*bad, fmt.Sprintf("%s = %s", path, v.String()))
		}
	case map[string]any:
		// Sorted so a failure names the same field first on every run.
		for _, k := range sorted.Keys(v) {
			walkValue(v[k], path+"."+k, bad)
		}
	case []any:
		for i, e := range v {
			walkValue(e, fmt.Sprintf("%s[%d]", path, i), bad)
		}
	}
}
