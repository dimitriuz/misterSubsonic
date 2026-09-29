package ui

import "strings"

func splitLines(s string) []string { return strings.Split(s, "\n") }
func splitWords(s string) []string { return strings.Fields(s) }
