//go:build race

package logger

// raceEnabled skips the allocation budget: the race detector allocates on its
// own and changes the counts.
const raceEnabled = true
