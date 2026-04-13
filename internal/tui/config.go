package tui

// MaxDisplayedResults caps how many search results are kept in the model at
// once. This is a display/performance knob, not a search-quality one —
// raising it shows more rows but also widens the scroll window and grows
// the fuzzy-match allocation on every keystroke. 20 is comfortable on most
// terminals and keeps re-rank latency imperceptible even with tens of
// thousands of cached resources.
const MaxDisplayedResults = 20
