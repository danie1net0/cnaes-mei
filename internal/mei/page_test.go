package mei

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePageFromThePublishedHTML(test *testing.T) {
	file, err := os.Open(filepath.Join("testdata", "letter-a.html"))
	if err != nil {
		test.Fatalf("could not open the fixture: %s", err)
	}
	defer file.Close()

	activities, err := ParsePage(file, "A")
	if err != nil {
		test.Fatalf("ParsePage returned an error: %s", err)
	}

	if got, want := len(activities), 5; got != want {
		test.Fatalf("got %d activities, want %d: %v", got, want, activities)
	}
	want := Activity{
		CNAE:       4724500,
		Occupation: "ABATEDOR(A) DE AVES COM COMERCIALIZAÇÃO DO PRODUTO INDEPENDENTE",
		Letter:     "A",
	}
	if activities[0] != want {
		test.Errorf("first activity is %+v, want %+v", activities[0], want)
	}
	for _, activity := range activities {
		if activity.Letter != "A" {
			test.Errorf("activity %s has letter %q, want %q", activity, activity.Letter, "A")
		}
	}
}

func TestParsePageSkipsWhatIsNotAnActivity(test *testing.T) {
	const page = `<table>
		<tr><th>OCUPAÇÃO</th><th>CNAE</th></tr>
		<tr><td>SÓ UMA CÉLULA</td></tr>
		<tr><td></td><td>4724-5/00</td></tr>
		<tr><td>SEM CNAE</td><td>  </td></tr>
		<tr><td>AÇOUGUEIRO(A)&nbsp;INDEPENDENTE</td><td>4722-9/01</td></tr>
	</table>`

	activities, err := ParsePage(strings.NewReader(page), "A")
	if err != nil {
		test.Fatalf("ParsePage returned an error: %s", err)
	}

	want := []Activity{{CNAE: 4722901, Occupation: "AÇOUGUEIRO(A) INDEPENDENTE", Letter: "A"}}
	if len(activities) != len(want) || activities[0] != want[0] {
		test.Errorf("got %+v, want %+v", activities, want)
	}
}

func TestParsePageReadsNestedTablesOnce(test *testing.T) {
	const page = `<table><tr><td>
		<table><tr><td>AÇOUGUEIRO(A) INDEPENDENTE</td><td>4722-9/01</td></tr></table>
	</td></tr></table>`

	activities, err := ParsePage(strings.NewReader(page), "A")
	if err != nil {
		test.Fatalf("ParsePage returned an error: %s", err)
	}

	if got, want := len(activities), 1; got != want {
		test.Fatalf("got %d activities, want %d: %+v", got, want, activities)
	}
}

func TestParsePageWithoutATable(test *testing.T) {
	_, err := ParsePage(strings.NewReader(`<html><body><p>Em manutenção</p></body></html>`), "A")

	if !errors.Is(err, ErrNoTable) {
		test.Fatalf("got error %v, want %v", err, ErrNoTable)
	}
	if !strings.Contains(err.Error(), "letter A") {
		test.Errorf("error is %q, want it to name the letter", err)
	}
}

func TestParsePageWithAnUnreadableCNAE(test *testing.T) {
	const page = `<table>
		<tr><td>OCUPAÇÃO VÁLIDA</td><td>4722-9/01</td></tr>
		<tr><td>OCUPAÇÃO QUEBRADA</td><td>47-22</td></tr>
	</table>`

	activities, err := ParsePage(strings.NewReader(page), "A")

	if err == nil {
		test.Fatalf("got %+v, want an error", activities)
	}
	if !strings.Contains(err.Error(), "OCUPAÇÃO QUEBRADA") {
		test.Errorf("error is %q, want it to name the occupation", err)
	}

	// The good rows come back with the error: one typo in the source is not a
	// reason to drop the rest of the page.
	want := Activity{CNAE: 4722901, Occupation: "OCUPAÇÃO VÁLIDA", Letter: "A"}
	if len(activities) != 1 || activities[0] != want {
		test.Errorf("got %+v, want %+v", activities, []Activity{want})
	}
}

func TestParsePageWhenTheReaderFails(test *testing.T) {
	_, err := ParsePage(failingReader{}, "A")

	if err == nil {
		test.Fatal("got no error, want the failure of the reader")
	}
	if !errors.Is(err, errReader) {
		test.Errorf("got error %v, want it to wrap %v", err, errReader)
	}
}

var errReader = errors.New("the connection dropped")

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errReader }

func TestParsePageIgnoresWhatIsNotACell(test *testing.T) {
	// A <template> is one of the few elements a row may hold besides its
	// cells, so it is what a row with something else in it looks like.
	const page = `<table><tr><template><p>nota</p></template>` +
		`<td>AÇOUGUEIRO(A) INDEPENDENTE</td><td>4722-9/01</td></tr></table>`

	activities, err := ParsePage(strings.NewReader(page), "A")
	if err != nil {
		test.Fatalf("ParsePage returned an error: %s", err)
	}

	want := Activity{CNAE: 4722901, Occupation: "AÇOUGUEIRO(A) INDEPENDENTE", Letter: "A"}
	if len(activities) != 1 || activities[0] != want {
		test.Errorf("got %+v, want %+v", activities, []Activity{want})
	}
}
