package main

// Carry the timezone database inside the binary (etappe 66).
//
// The runtime image used to `apk add tzdata`, which was one of only two reasons the
// final stage needed the *target* architecture to execute — and therefore one of the
// two reasons an arm64 build had to run under emulation, where it failed twice (P2-7).
//
// The cost is roughly 450 KB of binary. What it buys is a runtime stage that runs no
// command at all, and timestamps that keep working on an image with no system tzdata,
// which is also true of any future move to a distroless base.
import _ "time/tzdata"
