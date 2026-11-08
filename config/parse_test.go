package config

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseYAMLMappingRequiresExplicitRoot(t *testing.T) {
	for _, test := range []struct {
		name, input string
	}{
		{name: "empty"}, {name: "comments", input: "# empty\n"},
		{name: "null", input: "null"}, {name: "null shorthand", input: "~"},
		{name: "document marker", input: "---\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseYAMLMapping([]byte(test.input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("non-mapping root accepted: %v", err)
			}
			if document, err := ParseYAML([]byte(test.input)); err != nil || len(document) != 0 {
				t.Fatalf("compatible root behavior changed: %v", err)
			}
		})
	}
	for _, input := range []string{"{}", "---\n{}", "values: [false, null, '', []]"} {
		got, err := ParseYAMLMapping([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		want, err := ParseYAML([]byte(input))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("mapping values changed", err)
		}
	}
}

func TestDotenvExpansionBudgets(t *testing.T) {
	large := strings.Repeat("x", MaxBytes/3)
	for _, tc := range []struct {
		name, text         string
		inherited, process map[string]string
	}{
		{"local_value", "X=" + large + "\nY=$X$X$X$X", nil, nil},
		{"process_value", "Y=$X$X$X$X", nil, map[string]string{"X": large}},
		{"inherited_value", "Y=$X$X$X$X", map[string]string{"X": large}, nil},
		{"aggregate", "A=$X\nB=$X\nC=$X\nD=$X", nil, map[string]string{"X": large}},
		{"inherited_aggregate", "B=$X\nC=$X", map[string]string{"A": large}, map[string]string{"X": large}},
		{"repeated_doubling", "X=x\n" + strings.Repeat("X=$X$X\n", 26), nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := ParseEnvironment(tc.text, tc.inherited, tc.process); !errors.Is(e, ErrInvalid) {
				t.Fatalf("unbounded expansion accepted: %v", e)
			}
		})
	}
	values, e := ParseEnvironment("X=abc\nX=$X$X\nY='$X'\nZ=\"\\$X\"", nil, nil)
	if e != nil || values["X"] != "abcabc" || values["Y"] != "$X" || values["Z"] != "$X" {
		t.Fatalf("bounded syntax regressed: %v %v", values, e)
	}
}

func TestDotenvPreservesUTF8Bytes(t *testing.T) {
	values, e := ParseEnvironment("X='網絡'\nY=$X\nZ=設備", nil, nil)
	if e != nil || values["X"] != "網絡" || values["Y"] != "網絡" || values["Z"] != "設備" {
		t.Fatalf("UTF8 changed: %v %v", values, e)
	}
}

func TestDotenvExpansionBudgetBoundaryAndReplacement(t *testing.T) {
	limitValue := strings.Repeat("x", MaxBytes-len("Y"))
	values, e := ParseEnvironment("Y=$X", nil, map[string]string{"X": limitValue})
	if e != nil || values["Y"] != limitValue {
		t.Fatalf("exact aggregate limit rejected: %v", e)
	}
	values, e = ParseEnvironment("Y=$X\nY=y", map[string]string{"Y": "old"}, map[string]string{"X": limitValue})
	if e != nil || values["Y"] != "y" {
		t.Fatalf("replacement was double-counted: %v", e)
	}
	if _, e = ParseEnvironment("Y=$X!", nil, map[string]string{"X": limitValue}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("one byte above aggregate limit accepted: %v", e)
	}
}
