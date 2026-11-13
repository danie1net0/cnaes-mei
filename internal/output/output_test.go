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

func TestWriteCSV(test *testing.T) {
	var out strings.Builder

	if err := Write(&out, CSV, sample); err != nil {
		test.Fatalf("Write returned an error: %s", err)
	}

	want := "cnae,ocupacao,letra\n" +
		"0111301,CULTIVO DE ARROZ,C\n" +
		"4722901,\"AÇOUGUEIRO(A), INDEPENDENTE\",A\n"
	if out.String() != want {
		test.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteTSV(test *testing.T) {
	var out strings.Builder

	if err := Write(&out, TSV, sample); err != nil {
		test.Fatalf("Write returned an error: %s", err)
	}

	want := "cnae\tocupacao\tletra\n" +
		"0111301\tCULTIVO DE ARROZ\tC\n" +
		"4722901\tAÇOUGUEIRO(A), INDEPENDENTE\tA\n"
	if out.String() != want {
		test.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteJSON(test *testing.T) {
	var out strings.Builder

	if err := Write(&out, JSON, sample); err != nil {
		test.Fatalf("Write returned an error: %s", err)
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
		test.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteNothing(test *testing.T) {
	for _, format := range Formats {
		test.Run(string(format), func(test *testing.T) {
			var out strings.Builder

			if err := Write(&out, format, nil); err != nil {
				test.Fatalf("Write returned an error: %s", err)
			}
			if out.Len() == 0 {
				test.Error("nothing was written, want at least the header or an empty list")
			}
		})
	}
}

func TestWriteUnknownFormat(test *testing.T) {
	err := Write(&strings.Builder{}, Format("xml"), sample)

	if err == nil {
		test.Fatal("got no error, want the unknown format")
	}
	if !strings.Contains(err.Error(), "csv, tsv, json") {
		test.Errorf("error is %q, want it to list the formats", err)
	}
}

func TestWriteWhenTheWriterFails(test *testing.T) {
	for _, format := range Formats {
		test.Run(string(format), func(test *testing.T) {
			err := Write(failingWriter{}, format, sample)

			if !errors.Is(err, errWriter) {
				test.Errorf("got error %v, want it to wrap %v", err, errWriter)
			}
		})
	}
}

func TestParseFormat(test *testing.T) {
	for _, testCase := range []struct {
		in   string
		want Format
	}{
		{"csv", CSV},
		{"CSV", CSV},
		{"tsv", TSV},
		{"json", JSON},
	} {
		test.Run(testCase.in, func(test *testing.T) {
			got, err := ParseFormat(testCase.in)
			if err != nil {
				test.Fatalf("ParseFormat(%q) returned an error: %s", testCase.in, err)
			}
			if got != testCase.want {
				test.Errorf("ParseFormat(%q) got %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestParseFormatUnknown(test *testing.T) {
	_, err := ParseFormat("xml")

	if err == nil {
		test.Fatal("got no error, want the unknown format")
	}
	if !strings.Contains(err.Error(), "csv, tsv, json") {
		test.Errorf("error is %q, want it to list the formats", err)
	}
}

func TestNames(test *testing.T) {
	if got, want := Names(), "csv, tsv, json"; got != want {
		test.Errorf("Names() got %q, want %q", got, want)
	}
}

var errWriter = errors.New("the disk is full")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriter }
