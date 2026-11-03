package mei

import (
	"strings"
	"testing"
)

func TestParseCNAE(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{"as published", "4724-5/00", 4724500},
		{"only digits", "4724500", 4724500},
		{"leading zero", "0111-3/01", 111301},
		{"other punctuation", "4724-5-00", 4724500},
		{"surrounded by spaces", " 4724-5/00 ", 4724500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCNAE(tc.raw)
			if err != nil {
				t.Fatalf("ParseCNAE(%q) returned an error: %s", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ParseCNAE(%q) got %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseCNAEErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"too few digits", "4724-5", "has 5 digits"},
		{"too many digits", "4724-5/001", "more than 7 digits"},
		{"no digits", "CNAE", "has 0 digits"},
		{"empty", "", "has 0 digits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCNAE(tc.raw)
			if err == nil {
				t.Fatalf("ParseCNAE(%q) got %d, want an error", tc.raw, got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseCNAE(%q) error is %q, want it to mention %q", tc.raw, err, tc.want)
			}
		})
	}
}

func TestFormatCNAE(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{
		{4724500, "4724500"},
		{111301, "0111301"},
		{0, "0000000"},
	} {
		if got := FormatCNAE(tc.code); got != tc.want {
			t.Errorf("FormatCNAE(%d) got %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestParseCNAEAndFormatCNAERoundTrip(t *testing.T) {
	const raw = "0111-3/01"

	code, err := ParseCNAE(raw)
	if err != nil {
		t.Fatalf("ParseCNAE(%q) returned an error: %s", raw, err)
	}
	if got, want := FormatCNAE(code), "0111301"; got != want {
		t.Errorf("FormatCNAE(ParseCNAE(%q)) got %q, want %q", raw, got, want)
	}
}

func TestActivityString(t *testing.T) {
	activity := Activity{CNAE: 111301, Occupation: "CULTIVO DE ARROZ", Letter: "C"}

	if got, want := activity.String(), "0111301 CULTIVO DE ARROZ"; got != want {
		t.Errorf("String() got %q, want %q", got, want)
	}
}

func TestNormalizeSpace(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"trims", "  ABATEDOR  ", "ABATEDOR"},
		{"collapses runs", "ABATEDOR\n\t  DE   AVES", "ABATEDOR DE AVES"},
		{"non-breaking space", "ABATEDOR DE AVES", "ABATEDOR DE AVES"},
		{"empty", " \n ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeSpace(tc.in); got != tc.want {
				t.Errorf("normalizeSpace(%q) got %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
