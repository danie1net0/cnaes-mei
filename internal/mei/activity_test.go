package mei

import (
	"strings"
	"testing"
)

func TestParseCNAE(test *testing.T) {
	for _, testCase := range []struct {
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
		test.Run(testCase.name, func(test *testing.T) {
			got, err := ParseCNAE(testCase.raw)
			if err != nil {
				test.Fatalf("ParseCNAE(%q) returned an error: %s", testCase.raw, err)
			}
			if got != testCase.want {
				test.Errorf("ParseCNAE(%q) got %d, want %d", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestParseCNAEErrors(test *testing.T) {
	for _, testCase := range []struct {
		name string
		raw  string
		want string
	}{
		{"too few digits", "4724-5", "has 5 digits"},
		{"too many digits", "4724-5/001", "more than 7 digits"},
		{"no digits", "CNAE", "has 0 digits"},
		{"empty", "", "has 0 digits"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			got, err := ParseCNAE(testCase.raw)
			if err == nil {
				test.Fatalf("ParseCNAE(%q) got %d, want an error", testCase.raw, got)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				test.Errorf("ParseCNAE(%q) error is %q, want it to mention %q", testCase.raw, err, testCase.want)
			}
		})
	}
}

func TestFormatCNAE(test *testing.T) {
	for _, testCase := range []struct {
		code int
		want string
	}{
		{4724500, "4724500"},
		{111301, "0111301"},
		{0, "0000000"},
	} {
		if got := FormatCNAE(testCase.code); got != testCase.want {
			test.Errorf("FormatCNAE(%d) got %q, want %q", testCase.code, got, testCase.want)
		}
	}
}

func TestParseCNAEAndFormatCNAERoundTrip(test *testing.T) {
	const raw = "0111-3/01"

	code, err := ParseCNAE(raw)
	if err != nil {
		test.Fatalf("ParseCNAE(%q) returned an error: %s", raw, err)
	}
	if got, want := FormatCNAE(code), "0111301"; got != want {
		test.Errorf("FormatCNAE(ParseCNAE(%q)) got %q, want %q", raw, got, want)
	}
}

func TestActivityString(test *testing.T) {
	activity := Activity{CNAE: 111301, Occupation: "CULTIVO DE ARROZ", Letter: "C"}

	if got, want := activity.String(), "0111301 CULTIVO DE ARROZ"; got != want {
		test.Errorf("String() got %q, want %q", got, want)
	}
}

func TestNormalizeSpace(test *testing.T) {
	for _, testCase := range []struct {
		name string
		in   string
		want string
	}{
		{"trims", "  ABATEDOR  ", "ABATEDOR"},
		{"collapses runs", "ABATEDOR\n\t  DE   AVES", "ABATEDOR DE AVES"},
		{"non-breaking space", "ABATEDOR DE AVES", "ABATEDOR DE AVES"},
		{"empty", " \n ", ""},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			if got := normalizeSpace(testCase.in); got != testCase.want {
				test.Errorf("normalizeSpace(%q) got %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}
