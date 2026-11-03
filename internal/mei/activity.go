// Package mei collects the occupations a Brazilian individual microentrepreneur
// (MEI) is allowed to carry out, as published by the federal government, and the
// CNAE code of each one.
package mei

import (
	"fmt"
	"strings"
)

// cnaeDigits is the number of digits in a CNAE subclass code, the format the
// Receita Federal uses in its own data: four for the class, one for the check
// digit and two for the subclass.
const cnaeDigits = 7

// Activity is an occupation allowed for an MEI, with the CNAE code it maps to.
//
// The same CNAE often serves several occupations ("vendor of X" and "vendor of
// Y" sharing a retail code), so a CNAE may appear more than once in a list of
// activities. Callers that only need to know whether a CNAE is allowed should
// deduplicate; callers building a catalogue keep the pairs.
type Activity struct {
	CNAE       int    `json:"cnae"`
	Occupation string `json:"ocupacao"`
	Letter     string `json:"letra"`
}

// String formats the activity for logs and error messages.
func (a Activity) String() string {
	return fmt.Sprintf("%s %s", FormatCNAE(a.CNAE), a.Occupation)
}

// ParseCNAE reads a CNAE code the way the government publishes it — "4724-5/00",
// "4724500" or with any other punctuation — and returns it as the integer the
// relational model stores.
func ParseCNAE(raw string) (int, error) {
	var code int
	var digits int
	for _, r := range raw {
		if r < '0' || r > '9' {
			continue
		}
		digits++
		if digits > cnaeDigits {
			return 0, fmt.Errorf("CNAE %q has more than %d digits", raw, cnaeDigits)
		}
		code = code*10 + int(r-'0')
	}
	if digits != cnaeDigits {
		return 0, fmt.Errorf("CNAE %q has %d digits, expected %d", raw, digits, cnaeDigits)
	}
	return code, nil
}

// FormatCNAE writes a CNAE code back as seven digits, keeping the leading zeros
// that the integer form drops.
func FormatCNAE(code int) string {
	return fmt.Sprintf("%0*d", cnaeDigits, code)
}

// normalizeSpace collapses every run of whitespace — including the non-breaking
// spaces the government's HTML is full of — into a single blank, and trims the
// ends.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}
