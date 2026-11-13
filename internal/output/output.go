// Package output writes a list of activities in the formats the callers of
// this tool need: a spreadsheet-friendly CSV, the tab-separated form that
// PostgreSQL's COPY reads, and JSON for anything else.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gitlab.com/danie1net0/cnaes-mei/internal/mei"
)

// Format is how the activities are written.
type Format string

// The supported formats.
const (
	CSV  Format = "csv"
	TSV  Format = "tsv"
	JSON Format = "json"
)

// Formats lists every supported format, in the order the help text shows them.
var Formats = []Format{CSV, TSV, JSON}

// ParseFormat reads a format by name.
func ParseFormat(name string) (Format, error) {
	for _, format := range Formats {
		if strings.EqualFold(name, string(format)) {
			return format, nil
		}
	}
	return "", fmt.Errorf("unknown format %q, expected one of %s", name, Names())
}

// Names lists the format names, for help texts and error messages.
func Names() string {
	names := make([]string, len(Formats))
	for index, format := range Formats {
		names[index] = string(format)
	}
	return strings.Join(names, ", ")
}

// Write writes the activities in the given format.
//
// The CNAE goes out as the seven digits the Receita Federal publishes, as text:
// as a number it would lose the leading zero of codes such as 0111-3/01.
func Write(writer io.Writer, format Format, activities []mei.Activity) error {
	switch format {
	case CSV:
		return writeSeparated(writer, ',', activities)
	case TSV:
		return writeSeparated(writer, '\t', activities)
	case JSON:
		return writeJSON(writer, activities)
	}
	return fmt.Errorf("unknown format %q, expected one of %s", format, Names())
}

// writeSeparated writes the records with a header row, which both a
// spreadsheet and PostgreSQL's COPY ... WITH (FORMAT csv, HEADER) understand.
func writeSeparated(writer io.Writer, comma rune, activities []mei.Activity) error {
	out := csv.NewWriter(writer)
	out.Comma = comma

	records := [][]string{{"cnae", "ocupacao", "letra"}}
	for _, activity := range activities {
		records = append(records, []string{mei.FormatCNAE(activity.CNAE), activity.Occupation, activity.Letter})
	}
	if err := out.WriteAll(records); err != nil {
		return fmt.Errorf("could not write the records: %w", err)
	}
	return nil
}

// jsonActivity mirrors mei.Activity with the CNAE as text.
type jsonActivity struct {
	CNAE       string `json:"cnae"`
	Occupation string `json:"ocupacao"`
	Letter     string `json:"letra"`
}

func writeJSON(writer io.Writer, activities []mei.Activity) error {
	records := make([]jsonActivity, 0, len(activities))
	for _, activity := range activities {
		records = append(records, jsonActivity{
			CNAE:       mei.FormatCNAE(activity.CNAE),
			Occupation: activity.Occupation,
			Letter:     activity.Letter,
		})
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(records); err != nil {
		return fmt.Errorf("could not write the JSON: %w", err)
	}
	return nil
}
