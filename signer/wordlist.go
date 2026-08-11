package signer

import (
	"strings"
	_ "embed"
)

//go:embed wordlist_english.txt
var wordlistFile string

var wordlist []string
var wordIndex map[string]int

func init() {
	wordlist = strings.Split(strings.TrimRight(wordlistFile, "\n"), "\n")
	wordIndex = make(map[string]int, len(wordlist))
	for i, w := range wordlist {
		wordIndex[w] = i
	}
}
