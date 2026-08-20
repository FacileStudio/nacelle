package main

import (
	"fmt"

	"github.com/FacileStudio/nacelle"
	"github.com/FacileStudio/nacelle/tools"
)

// webTools are the two that reach past this machine: search, when an instance
// has been named, and fetch, unless it has been turned off.
//
// They are built together because they are one capability in two halves — a
// search that returns a sentence per hit is only useful if something can then
// read the page — and because the settings behind them are one group in the
// config file.
//
// WebSearch's error goes back unwrapped, unlike the other two here. It already
// names the endpoint and what is wrong with it, and a second "building the web
// search tool" in front of that says nothing the reader has not just read.
func webTools(config Config) ([]nacelle.Tool, error) {
	searching, err := tools.WebSearch(*config.Search)
	if err != nil {
		return nil, err
	}
	if !*config.Fetch {
		return searching, nil
	}

	reading, err := tools.WebFetch()
	if err != nil {
		return nil, fmt.Errorf("building the web fetch tool: %w", err)
	}
	return append(searching, reading...), nil
}
