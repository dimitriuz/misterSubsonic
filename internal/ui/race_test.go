//go:build race

package ui

// underRace is true when the tests run with -race, which makes the frame
// loops several times slower: the heavy ones run fewer frames then.
const underRace = true
