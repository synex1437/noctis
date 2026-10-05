//go:build race

package main

// raceDetector tells a test that times work it does itself that the race detector, which makes such work
// several times slower, is on.
const raceDetector = true
