package output

import (
	"errors"
	"strings"
	"testing"

	"gitlab.com/danie1net0/cnaes-mei/internal/mei"
)

// sample is a pair of activities with a CNAE that keeps a leading zero and an
// occupation with a comma, which is what tells the formats apart.
var sample = []mei.Activity{
	{CNAE: 111301, Occupation: "CULTIVO DE ARROZ", Letter: "C"},
	{CNAE: 4722901, Occupation: "AÇOUGUEIRO(A), INDEPENDENTE", Letter: "A"},
}

func TestWriteCSV(t *testing.T) {
	var out strings.Builder

	if err := Write(&out, CSV, sample); err != nil {
		t.Fatalf("Write returned an error: %s", err)
	}

	want := "cnae,ocupacao,letra\n" +
		"0111301,CULTIVO DE ARROZ,C\n" +
		"4722901,\"AÇOUGUEIRO(A), INDEPENDENTE\",A\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteTSV(t *testing.T) {
	var out strings.Builder

	if err := Write(&out, TSV, sample); err != nil {
		t.Fatalf("Write returned an error: %s", err)
	}

	want := "cnae\tocupacao\tletra\n" +
		"0111301\tCULTIVO DE ARROZ\tC\n" +
		"4722901\tAÇOUGUEIRO(A), INDEPENDENTE\tA\n"
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteJSON(t *testing.T) {
	var out strings.Builder

	if err := Write(&out, JSON, sample); err != nil {
		t.Fatalf("Write returned an error: %s", err)
	}

	want := `[
  {
    "cnae": "0111301",
    "ocupacao": "CULTIVO DE ARROZ",
    "letra": "C"
  },
  {
    "cnae": "4722901",
    "ocupacao": "AÇOUGUEIRO(A), INDEPENDENTE",
    "letra": "A"
  }
]
`
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteNothing(t *testing.T) {
	for _, format := range Formats {
		t.Run(string(format), func(t *testing.T) {
			var out strings.Builder

			if err := Write(&out, format, nil); err != nil {
				t.Fatalf("Write returned an error: %s", err)
			}
			if out.Len() == 0 {
				t.Error("nothing was written, want at least the header or an empty list")
			}
		})
	}
}

func TestWriteUnknownFormat(t *testing.T) {
	err := Write(&strings.Builder{}, Format("xml"), sample)

	if err == nil {
		t.Fatal("got no error, want the unknown format")
	}
	if !strings.Contains(err.Error(), "csv, tsv, json") {
		t.Errorf("error is %q, want it to list the formats", err)
	}
}

func TestWriteWhenTheWriterFails(t *testing.T) {
	for _, format := range Formats {
		t.Run(string(format), func(t *testing.T) {
			err := Write(failingWriter{}, format, sample)

			if !errors.Is(err, errWriter) {
				t.Errorf("got error %v, want it to wrap %v", err, errWriter)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Format
	}{
		{"csv", CSV},
		{"CSV", CSV},
		{"tsv", TSV},
		{"json", JSON},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseFormat(tc.in)
			if err != nil {
				t.Fatalf("ParseFormat(%q) returned an error: %s", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseFormat(%q) got %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseFormatUnknown(t *testing.T) {
	_, err := ParseFormat("xml")

	if err == nil {
		t.Fatal("got no error, want the unknown format")
	}
	if !strings.Contains(err.Error(), "csv, tsv, json") {
		t.Errorf("error is %q, want it to list the formats", err)
	}
}

func TestNames(t *testing.T) {
	if got, want := Names(), "csv, tsv, json"; got != want {
		t.Errorf("Names() got %q, want %q", got, want)
	}
}

var errWriter = errors.New("the disk is full")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriter }
