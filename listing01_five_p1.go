//go:build ignore

// This sample program is the single logical processor version of
// listing01_five.go.
//
// listing01_five.go deliberately did NOT pin GOMAXPROCS, so on a multi core
// machine the scheduler spreads the goroutines over several logical
// processors and work stealing scrambles the output.  That is realistic,
// but it makes the queue behaviour impossible to read.
//
// This version locks the scheduler down to exactly ONE logical processor
// with runtime.GOMAXPROCS(1), which removes the multi P noise -- no work
// stealing, no cross core parallelism -- so the only thing left to observe
// is the queue order inside a single P.
//
// The mechanism under test:
//
//	newproc -> runqput(pp, g, next=true)
//
// runqput puts the new goroutine into the P's `runnext` slot (the "VIP"
// slot).  If the slot was already taken, the previous occupant is pushed
// into the tail of the P's local run queue:
//
//	// Kick the old runnext out to the regular run queue.
//
// So `runnext` behaves like a LIFO stack (the last goroutine created wins
// the slot) while the local run queue is a 256 entry ring buffer that is
// drained in FIFO order.
//
// Creating G1..G5 on a single P therefore leaves:
//
//	runnext (VIP, LIFO) : G5
//	local ring (FIFO)   : G1 -> G2 -> G3 -> G4
//
// and the scheduler, which takes runnext first and then the head of the
// ring, runs them in the order:
//
//	G5 -> G1 -> G2 -> G3 -> G4
//
// The program prints that prediction as it goes and then checks the
// prediction against what actually happened.
//
// IMPORTANT: none of this ordering is a language guarantee.  It is an
// implementation detail of the Go runtime that can change between
// versions, between machines, and between builds.  A race enabled build
// randomizes the scheduler outright:
//
//	const randomizeScheduler = raceenabled
//
// which makes runqput skip the runnext slot about half the time, so the
// order changes.  Never rely on the order; rely on sync.WaitGroup for
// "everyone finished" and on channels for "the data is synchronized".
//
// A second thing to watch is the 10ms scheduler time slice:
//
//	const forcePreemptNS = 10 * 1000 * 1000 // 10ms
//
// G3 is deliberately CPU bound and runs for far longer than that, so the
// scheduler preempts it in the middle of its computation.  A preempted
// goroutine is pushed onto the GLOBAL run queue
// (gopreempt_m -> goschedImpl(gp, true) -> globrunqput(gp)), so G4, which
// is still sitting in the LOCAL ring, gets to run first.  That is why
// G4's output appears before G3's result line even though G3 was created
// first and started first.
//
// Run it:
//
//	go run listing01_five_p1.go                  # G3 heavy (default)
//	go run listing01_five_p1.go -heavy=false     # G3 light, pure queue order
//
// The //go:build ignore constraint keeps this file out of the package, so
// `go build ./...` and `go vet ./...` skip it and the other listings in
// this directory keep their own func main.  Files named explicitly on the
// command line are exempt from build constraints, so `go run
// listing01_five_p1.go` and `go vet listing01_five_p1.go` still work.
package main

import (
	"flag"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

// start is the reference point used to timestamp every output line.
var start = time.Now()

// wg is used to wait for the program to finish.
var wg sync.WaitGroup

// orderMu guards order.  The five goroutines run concurrently, so the
// slice that records who printed first needs a lock.
var orderMu sync.Mutex

// order records the label of every goroutine in the order it printed its
// first line.
var order []string

// onceGx guarantees that a goroutine records its label exactly once.
var (
	onceG1 sync.Once
	onceG2 sync.Once
	onceG3 sync.Once
	onceG4 sync.Once
	onceG5 sync.Once
)

// predictedOrder is the hand written prediction of the order in which the
// goroutines will print their first line.  It is derived from reading
// runtime/proc.go -- it is NOT read back from the runtime.
const predictedOrder = "G5 → G1 → G2 → G3 → G4"

// tag builds the "[Gx +12.345ms] " prefix that identifies the goroutine
// and the elapsed wall clock time.
func tag(label string) string {
	ms := float64(time.Since(start).Microseconds()) / 1000.0
	return fmt.Sprintf("[%s +%9.3fms] ", label, ms)
}

// firstPrint prints a goroutine's identity line and records its label.
//
// The append and the write happen under the same lock, so the recorded
// order is exactly the order in which the identity lines appear in the
// output -- which is what makes the stage 4 comparison meaningful.
func firstPrint(label, msg string) {
	orderMu.Lock()
	defer orderMu.Unlock()

	order = append(order, label)
	fmt.Println(tag(label), msg)
}

// predict prints the hand written prediction of P0's queue state after
// the given number of goroutines have been created.
//
// These values come from reading runtime/proc.go -- they are predictions,
// not a runtime readout.  The runtime does not expose the runnext slot or
// the local run queue to user code.
func predict(n int, name, runnext, ring, note string) {
	fmt.Printf("[创建 #%d] go func(){%s}\n", n, name)
	fmt.Println("   预测 P0 队列状态 →")
	fmt.Printf("       runnext (VIP 槽, LIFO) : %s", runnext)
	if note != "" {
		fmt.Printf("      ← %s", note)
	}
	fmt.Println()
	fmt.Printf("       local ring (FIFO)      : %s\n", ring)
}

// main is the entrypoint for all Go programs.
func main() {
	// Lock the scheduler down to a single logical processor.  This is the
	// whole point of this file: one P, no work stealing, no cross core
	// parallelism -- only the queue order inside one P.
	runtime.GOMAXPROCS(1)

	// -heavy turns G3's long CPU bound computation on or off.  Run with
	// -heavy=false first to see the pure queue order with nothing else
	// going on.
	heavy := flag.Bool("heavy", true, "让 G3 做 CPU 密集筛素数 (远超 10ms 时间片), 以观察调度器抢占")
	flag.Parse()

	fmt.Println("--- 阶段 1: 设定 ---")
	fmt.Printf("main: runtime.GOMAXPROCS(1) 已锁定 → 实际 P 数 = %d\n", runtime.GOMAXPROCS(0))
	fmt.Printf("main: 机器逻辑核 runtime.NumCPU() = %d\n", runtime.NumCPU())
	fmt.Println("main: 本次实验只用 P0 一个逻辑处理器, 隔离掉 work stealing / 核间并行噪音")
	fmt.Println("main: 唯一目标: 看清 runnext VIP 槽 (LIFO) + 本地环形队列 (FIFO) 的入队/出队顺序")
	fmt.Printf("main: heavy 模式 = %v\n", *heavy)
	if *heavy {
		fmt.Println("main:   G3 会做 CPU 密集筛素数, 远超 10ms 时间片 → 预期能看到抢占")
	} else {
		fmt.Println("main:   G3 只打一行就结束 → 本次只看纯队列顺序, 没有任何抢占")
	}
	fmt.Println()

	// Add a count of five, one for each goroutine.
	wg.Add(5)

	// G1 is the lightest goroutine.  It captures the alphabet from the
	// enclosing scope and replays it three times using its own loop
	// variable.  It finishes in well under a time slice, so its output
	// shows up as one unbroken block.
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		onceG1.Do(func() {
			firstPrint("G1", "G1 START — 打印大写字母表 (A-Z x 3 轮)")
		})
		for round := 1; round <= 3; round++ {
			fmt.Println(tag("G1"), alphabet, " (round", round, ")")
		}
	}()
	fmt.Println("main: created G1 (轻量, 大写字母表 x 3 轮)")
	predict(1, "G1", "G1", "(空)", "")

	// G2 is the medium weight goroutine.  It captures the line count from
	// the enclosing scope and derives every number it prints from it.
	numberCount := 50
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		onceG2.Do(func() {
			firstPrint("G2", "G2 START — 打印数字 01..50, 每行 10 个")
		})
		for first := 1; first <= numberCount; first += 10 {
			line := ""
			for n := first; n < first+10 && n <= numberCount; n++ {
				line += fmt.Sprintf("%02d ", n)
			}
			fmt.Println(tag("G2"), line)
		}
	}()
	fmt.Println("main: created G2 (中等, 数字 01..50, 每行 10 个)")
	predict(2, "G2", "G2", "G1", "旧 runnext (G1) 被踢进 ring 尾部")

	// G3 is the heavy one.  It captures the sieve limit from the
	// enclosing scope and burns far more CPU than a single ~10ms time
	// slice allows, so the scheduler will preempt it mid computation.
	// The computation itself prints nothing -- only the identity line
	// and one result line -- so you can see exactly when the CPU was
	// taken away from it and handed to another goroutine.
	sieveLimit := 20000000
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		onceG3.Do(func() {
			if *heavy {
				firstPrint("G3", "G3 START — CPU 密集筛素数 (up to 20000000), 远超 10ms 时间片")
			} else {
				firstPrint("G3", "G3 START — 轻量模式 (-heavy=false), 只打这一行就结束")
			}
		})
		if !*heavy {
			return
		}

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
	fmt.Println("main: created G3 (重, CPU 密集筛素数, ~140ms >> 10ms 时间片)")
	predict(3, "G3", "G3", "G1 → G2", "旧 runnext (G2) 被踢进 ring 尾部")

	// G4 prints a symbol pattern that looks nothing like the other three.
	// It captures both the pattern and the row count from the enclosing
	// scope.  In heavy mode G4 is the goroutine that gets to run while
	// G3 is parked in the global queue.
	pattern := "#=+*#=+*#=+*#=+*#=+*"
	patternRows := 40
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		onceG4.Do(func() {
			firstPrint("G4", "G4 START — 打印 ASCII 符号图案 #=+*")
		})
		for row := 1; row <= patternRows; row++ {
			fmt.Printf("%s%-2d %s\n", tag("G4"), row, pattern)
		}
	}()
	fmt.Println("main: created G4 (中等, ASCII 符号图案 #=+*, 40 行)")
	predict(4, "G4", "G4", "G1 → G2 → G3", "旧 runnext (G3) 被踢进 ring 尾部")

	// G5 uses literals instead of captured variables.  It is created
	// LAST, so it is the one sitting in the runnext VIP slot when main
	// blocks -- and therefore the one that runs FIRST.
	go func() {
		// Schedule the call to Done to tell main we are done.
		defer wg.Done()

		onceG5.Do(func() {
			firstPrint("G5", "G5 START — 中文 + emoji")
		})
		for line := 1; line <= 10; line++ {
			fmt.Printf("%s第 %d 行：你好，Go 并发 🐹\n", tag("G5"), line)
		}
	}()
	fmt.Println("main: created G5 (轻量, 中文 + emoji, 10 行) -- 最后创建, 预期第一个跑")
	predict(5, "G5", "G5", "G1 → G2 → G3 → G4", "最后创建的抢到 VIP 槽")

	// Wait for the goroutines to finish.
	fmt.Println()
	fmt.Println("--- 阶段 2: main 阻塞在 wg.Wait(), P0 开始取活 ---")
	fmt.Println("调度器取活顺序: ① runnext (VIP 槽)  →  ② local ring 队头 (FIFO)")
	fmt.Printf("预测执行顺序   : %s\n", predictedOrder)
	fmt.Println("预测第一个打印 : G5")
	fmt.Println()

	wg.Wait()

	fmt.Println()
	fmt.Println("--- 阶段 3: wg.Wait() 返回, 5 个 goroutine 全部结束 ---")
	if *heavy {
		fmt.Println("main: 重点检查 → G3 的 done 行是否排在 G4 的 40 行之后 (被抢占的证据)")
	} else {
		fmt.Println("main: 重点检查 → G5 的 START 行是否紧跟在 main: created G5 之后 (runnext 抢到槽的证据)")
	}
	fmt.Println()

	// Stage 4: compare the prediction against what actually happened.
	actualOrder := strings.Join(order, " → ")
	fmt.Println("--- 阶段 4: 验证 ---")
	fmt.Printf("预测的第一个打印顺序 : %s\n", predictedOrder)
	fmt.Printf("实际的第一个打印顺序 : %s\n", actualOrder)
	if actualOrder == predictedOrder {
		fmt.Println("结论: ✅ 一致 —— 单 P 下 runnext(LIFO) + ring(FIFO) 的预测成立")
	} else {
		fmt.Println("结论: ❌ 不一致 —— 调度器行为与手写预测不同")
		fmt.Println("      (顺序是实现细节: 换 Go 版本 / 换机器 / 加 -race 都可能改变它)")
	}
	fmt.Println()
	fmt.Println("TerminatingProgram")
}
