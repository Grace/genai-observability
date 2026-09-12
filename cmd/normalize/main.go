package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/grace/genai-observability/internal/normalize"
)

func main() {
	file := flag.String("file", "", "normalization request JSON file; defaults to stdin")
	flag.Parse()
	var dec *json.Decoder
	if *file == "" {
		dec = json.NewDecoder(os.Stdin)
	} else {
		f, err := os.Open(*file)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		dec = json.NewDecoder(f)
	}
	var req normalize.Request
	if err := dec.Decode(&req); err != nil {
		fatal(err)
	}
	report, err := normalize.Run(req)
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fatal(err)
	}
	if len(report.Errors) > 0 {
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
