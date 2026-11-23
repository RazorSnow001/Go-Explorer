//go:build ignore

// This sample program demonstrates how the Go scheduler's runnext slot
// and the per-P local run queue interact with goroutine creation order.
//
// It is a derivative of listing01.go: instead of two goroutines printing
// the alphabet, five goroutines print five visually distinct things so
// that you can tell them apart in the output and watch the scheduler.
//
// What the runtime does with each `go func(){}`:
//
//	newproc -> runqput(pp, g, next=true)
//
//	runqput puts the new goroutine into the P's `runnext` slot (the "VIP"
//	slot).  If the slot was already taken, the previous occupant is pushed
//	into the tail of the P's local run queue -- a 256 slot ring buffer
//	that is drained in FIFO order.
//
//	So after creating G1..G5 on a single P:
//
//		runnext (LIFO, VIP slot) : G5
//		local ring (FIFO)         : G1 -> G2 -> G3 -> G4
//
//	When main blocks on wg.Wait(), the scheduler takes runnext first, so
//	the LAST goroutine created runs FIRST.  Once G5 is done, the ring
//	buffer is drained in creation order.
//
// Expected execution order (single P):
//
//	G5 -> G1 -> G2 -> G3 -> G4
//
//	G5 grabbed the VIP slot.  G1 was kicked out of it and went to the
//	head of the ring buffer, so it runs second.
//
//	G3 is deliberately CPU bound and runs for far longer than the ~10ms
//	scheduler time slice, so it gets preempted in the middle of its
//	computation.  That is why G4's output shows up BEFORE G3's result
//	line: G3 started first but was pushed to the back of the queue, G4
//	ran in its place, and G3 only finished once it got the CPU back.
//
// Each line is prefixed with the goroutine label and the elapsed time so
// that the interleaving is obvious.
//
// IMPORTANT: none of this ordering is a guarantee.  Build with -race and
// the runtime randomizes the scheduler (runtime/proc.go:
//
//	const randomizeScheduler = raceenabled
//
//	) so runnext is only used about half the time and the order changes.
//
// Run it with a single P for the cleanest demonstration:
//
//	GOMAXPROCS=1 go run listing01_five.go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// start is the reference point used to timestamp every output line.
var start = time.Now()

// tag builds the "[Gx +12.345ms] " prefix that identifies the goroutine
// and the elapsed wall clock time.
func tag(label string) string {
	ms := float64(time.Since(start).Microseconds()) / 1000.0
	return fmt.Sprintf("[%s +%9.3fms] ", label, ms)
}

// main is the entrypoint for all Go programs.
func main() {
	// Report how many logical processors the scheduler has.  This program
	// does NOT pin GOMAXPROCS itself, so you can compare the default
	// (one P per CPU core) against a single P with:
	//
	//	GOMAXPROCS=1 go run listing01_five.go
	//
	// With more than one P the scheduler can also steal work, so the
	// order below becomes genuinely nondeterministic.
	fmt.Printf("main: GOMAXPROCS = %d (logical processors available)\n", runtime.GOMAXPROCS(0))
	fmt.Printf("main: NumCPU     = %d\n", runtime.NumCPU())
	fmt.Println()
	fmt.Println("main: creating goroutines in the order G1 -> G2 -> G3 -> G4 -> G5")
	fmt.Println("main: hypothesis on a single P:")
	fmt.Println("main:     runnext VIP slot (LIFO) holds the LAST one created = G5")
	fmt.Println("main:     local ring buffer (FIFO) holds G1 -> G2 -> G3 -> G4")
	fmt.Println("main:     so expect: G5 -> G1 -> G2 -> G3 -> G4")
	fmt.Println()

	// wg is used to wait for the program to finish.
	// Add a count of five, one for each goroutine.
	var wg sync.WaitGroup
	wg.Add(5)

	// G1 is the lightest goroutine.  It captures the alphabet from the
	// enclosing scope and replays it three times using its own loop
	// variable.  It finishes in well under a time slice, so its output
	// shows up as one unbroken block.
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		for round := 1; round <= 3; round++ {
			fmt.Println(tag("G1"), alphabet, " (round", round, ")")
		}
	}()
	fmt.Println("main: created G1 (light, uppercase alphabet, 3 rounds)")

	// G2 is the medium weight goroutine.  It captures the line count from
	// the enclosing scope and derives every number it prints from it.
	numberCount := 50
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		for n := 1; n <= numberCount; n++ {
			fmt.Printf("%s%02d ", tag("G2"), n)
			if n%10 == 0 {
				fmt.Println()
			}
		}
	}()
	fmt.Println("main: created G2 (medium, 50 numbers, 10 per line)")

	// G3 is the heavy one.  It captures the sieve limit from the
	// enclosing scope and burns far more CPU than a single ~10ms time
	// slice allows, so the scheduler will preempt it mid computation.
	// The computation itself prints nothing -- only one start marker and
	// one result line -- so you can see exactly when the CPU was taken
	// away from it and handed to another goroutine.
	sieveLimit := 20000000
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		fmt.Println(tag("G3"), "start: sieving primes up to", sieveLimit)

		sieve := make([]bool, sieveLimit)
		count := 0
		for i := 2; i < sieveLimit; i++ {
			if !sieve[i] {
				count++
				for j := i * i; j < sieveLimit; j += i {
					sieve[j] = true
				}
			}
		}

		fmt.Println(tag("G3"), "done: primes =", count)
	}()
	fmt.Println("main: created G3 (heavy, CPU bound sieve, ~140ms >> 10ms time slice)")

	// G4 prints a symbol pattern that looks nothing like the other three.
	// It captures both the pattern and the row count from the enclosing
	// scope.
	pattern := "#=+*#=+*#=+*#=+*#=+*"
	patternRows := 40
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		for row := 1; row <= patternRows; row++ {
			fmt.Printf("%s%-2d %s\n", tag("G4"), row, pattern)
		}
	}()
	fmt.Println("main: created G4 (symbols, 40 rows of #=+*)")

	// G5 uses literals instead of captured variables.  It is created
	// LAST, so it is the one sitting in the runnext VIP slot when main
	// blocks -- and therefore the one that runs FIRST.
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		for line := 1; line <= 10; line++ {
			fmt.Printf("%s第 %d 行：你好，Go 并发 🐹\n", tag("G5"), line)
		}
	}()
	fmt.Println("main: created G5 (chinese + emoji, 10 lines) -- LAST created, expect it FIRST")

	// Wait for the goroutines to finish.
	fmt.Println()
	fmt.Println("main: all five goroutines created, calling wg.Wait()")
	wg.Wait()

	fmt.Println()
	fmt.Println("main: all goroutines finished")
	fmt.Println("TerminatingProgram")
}
