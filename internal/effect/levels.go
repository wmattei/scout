package effect

// Level controls toast + pill colouring. Aligned with existing
// tui.Toast levels so migration doesn't reshuffle colour choices.
type Level int

const (
	LevelInfo Level = iota
	LevelSuccess
	LevelWarning
	LevelError
)
