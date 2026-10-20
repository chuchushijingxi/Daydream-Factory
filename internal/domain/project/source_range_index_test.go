package project

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
)

func TestSourceRangeIndexPreservesUnicodeValidationAndRepresentation(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	for _, spans := range [][]SourceSpan{
		{{"a", "甲🙂é"}, {"b", ""}, {"c", "第二段\n尾"}},
		{{"dup", "甲"}, {"dup", "更长的重复"}, {"c", "\xff尾"}},
		{},
	} {
		source := ScriptSource{Version: 3, Spans: spans}
		indexed := source.WithRangeIndex()
		a, _ := json.Marshal(source)
		b, _ := json.Marshal(indexed)
		if string(a) != string(b) || source.rangeIndex != nil {
			t.Fatal("index changed source facts")
		}
		ids := []string{"a", "b", "c", "dup", "missing"}
		for i := 0; i < 2000; i++ {
			ranges := []SourceRange{}
			for n := 0; n < 1+rng.Intn(3); n++ {
				ranges = append(ranges, SourceRange{Start: SourcePoint{SpanID: ids[rng.Intn(len(ids))], Offset: rng.Intn(10) - 1}, End: SourcePoint{SpanID: ids[rng.Intn(len(ids))], Offset: rng.Intn(10) - 1}})
			}
			if (source.ValidateRanges(ranges, false) == nil) != (indexed.ValidateRanges(ranges, false) == nil) {
				t.Fatal("validation differs", ranges)
			}
			a := source.NormalizeRanges(ranges)
			b := indexed.NormalizeRanges(ranges)
			if !reflect.DeepEqual(a, b) {
				t.Fatal("normalization differs", ranges, a, b)
			}
			if source.RangesWithin(ranges, ranges[:1]) != indexed.RangesWithin(ranges, ranges[:1]) {
				t.Fatal("containment differs")
			}
		}
	}
}
