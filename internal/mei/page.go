package mei

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// ErrNoTable is returned when a letter page carries no activity table at all,
// which means the page layout changed and the parser needs to be revisited.
var ErrNoTable = errors.New("no activity table found")

// headerOccupation and headerCNAE are the column titles of the published table.
// A row holding them is the header, not an activity.
const (
	headerOccupation = "OCUPAÇÃO"
	headerCNAE       = "CNAE"
)

// ParsePage reads one letter page of the government listing and returns the
// activities in it, along with an error describing the rows it could not read.
//
// Both values can be set at once, and that is the normal case when the source
// has a typo: the listing has published CNAEs with a digit missing, such as
// "602-5/02" for the depilator, and a single bad row is no reason to throw the
// other five hundred away. The caller decides whether to stop; mei.Scraper
// logs the rows by default and stops only in strict mode.
//
// Rows with fewer than two cells are layout noise and are skipped silently.
// A page with no table at all is a different matter — that is ErrNoTable, with
// no activities, because it means the layout changed.
func ParsePage(reader io.Reader, letter string) ([]Activity, error) {
	document, err := html.Parse(reader)
	if err != nil {
		return nil, fmt.Errorf("could not parse the HTML: %w", err)
	}

	var activities []Activity
	var problems []error
	var tables int

	// The document is walked once, so a table nested in another table does not
	// have its rows read twice.
	forEachNode(document, func(node *html.Node) {
		if node.Type != html.ElementNode {
			return
		}
		switch node.Data {
		case "table":
			tables++
		case "tr":
			activity, err := parseRow(node, letter)
			if err != nil {
				problems = append(problems, err)
				return
			}
			if activity != nil {
				activities = append(activities, *activity)
			}
		}
	})

	if tables == 0 {
		return nil, fmt.Errorf("%w for the letter %s", ErrNoTable, letter)
	}
	return activities, errors.Join(problems...)
}

// parseRow turns one table row into an activity. It returns a nil activity,
// and no error, for the rows that are not activities: the header and anything
// without both columns.
func parseRow(row *html.Node, letter string) (*Activity, error) {
	cells := rowCells(row)
	if len(cells) < 2 {
		return nil, nil
	}

	occupation, rawCNAE := cells[0], cells[1]
	if occupation == "" || rawCNAE == "" {
		return nil, nil
	}
	if strings.EqualFold(occupation, headerOccupation) && strings.EqualFold(rawCNAE, headerCNAE) {
		return nil, nil
	}

	code, err := ParseCNAE(rawCNAE)
	if err != nil {
		return nil, fmt.Errorf("occupation %q: %w", occupation, err)
	}
	return &Activity{CNAE: code, Occupation: occupation, Letter: letter}, nil
}

// rowCells returns the text of each cell of a row, whitespace normalized.
func rowCells(row *html.Node) []string {
	var cells []string
	for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
		if cell.Type != html.ElementNode || (cell.Data != "td" && cell.Data != "th") {
			continue
		}
		cells = append(cells, normalizeSpace(textOf(cell)))
	}
	return cells
}

// textOf concatenates the text under a node, ignoring the markup in between.
func textOf(node *html.Node) string {
	var text strings.Builder
	forEachNode(node, func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
			text.WriteString(" ")
		}
	})
	return text.String()
}

// forEachNode calls visit for the node and for every node below it, depth
// first.
func forEachNode(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		forEachNode(child, visit)
	}
}
