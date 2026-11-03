// Command cnaes-mei collects the occupations allowed for a Brazilian
// individual microentrepreneur (MEI), and the CNAE code of each one, from the
// listing published at gov.br.
package main

import (
	"os"

	"gitlab.com/danie1net0/cnaes-mei/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
