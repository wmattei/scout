package main

import "fmt"

// Version is set at build time via -ldflags (Phase 4). For Phase 1 it is a constant.
const Version = "0.0.0-phase1"

func main() {
	fmt.Printf("better-aws %s (scaffold — Phase 1 in progress)\n", Version)
}
