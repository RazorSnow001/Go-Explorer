//go:build ignore

// exp_goroutine_order.go —— 用来"看见" Go 调度器排队顺序的实验程序。
//
// 它是《Go in Action》第 138 页 Listing 6.7（GOMAXPROCS(2)）的放大版：
// 同样的 goroutine 骨架，但把 P 的个数、goroutine 的个数、每个 goroutine
// 的干活时长都做成环境变量，方便你不动代码就反复测试。
//
// 用法：
//
//	GOMAXPROCS=1 GOROUTINES=5 WORK_MS=50 go run exp_goroutine_order.go
//	GOMAXPROCS=2 GOROUTINES=5 WORK_MS=50 go run exp_goroutine_order.go
//	GOMAXPROCS=1 GOROUTINES=5 WORK_MS=0  go run exp_goroutine_order.go
//
// 三个旋钮：
//
//	GOMAXPROCS  P（逻辑处理器）的个数，默认 1
//	GOROUTINES  起几个 goroutine，默认 5
//	WORK_MS     每个 goroutine 每一步干活花多少毫秒，默认 50
//
// 看点：
//
//  1. GOMAXPROCS=1 时，"最后创建的 goroutine 最先跑"（runnext 槽位，LIFO），
//     其余按创建的先后顺序跑（FIFO，本地运行队列 256 格环形数组）。
//     5 个 goroutine 的预期首次执行顺序是：G5 -> G1 -> G2 -> G3 -> G4。
//  2. GOMAXPROCS=2 时，两个 P 同时跑，出现真正的并行交错，顺序不再可预测。
//  3. WORK_MS=0 时看不到时间片切换（活儿太短，一个片就跑完了）。
package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// start 是进程启动时刻，所有时间戳都相对它。
var start = time.Now()

// envInt 读取环境变量里的整数，取不到就用默认值。
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// goid 返回当前 goroutine 的 ID。
// Go 没有官方 API，这里用 runtime.Stack 的老办法（教学用，别用于生产）。
func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := string(buf[:n])
	s = s[len("goroutine "):]
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			s = s[:i]
			break
		}
	}
	id, _ := strconv.ParseUint(s, 10, 64)
	return id
}

// printer 串行化所有输出，并给每行打一个全局序号 + 相对时间。
// 这样"打印顺序"就是你真正观察到的执行顺序。
type printer struct {
	mu  sync.Mutex
	seq int
}

func (p *printer) logf(format string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	ms := float64(time.Since(start).Microseconds()) / 1000.0
	fmt.Printf("#%02d  t=%8.1fms  %s\n", p.seq, ms, fmt.Sprintf(format, a...))
}

// main 是所有 Go 程序的入口。
func main() {
	procs := envInt("GOMAXPROCS", 1)
	numG := envInt("GOROUTINES", 5)
	workMS := envInt("WORK_MS", 50)

	// 设置 P（逻辑处理器）的个数：这是调度器的并发名额。
	runtime.GOMAXPROCS(procs)

	pr := &printer{}
	pr.logf("main: GOMAXPROCS=%d  GOROUTINES=%d  WORK_MS=%d  NumCPU=%d  %s",
		procs, numG, workMS, runtime.NumCPU(), runtime.Version())

	var wg sync.WaitGroup
	wg.Add(numG)

	// 连续创建 numG 个 goroutine。
	// 注意：创建顺序 ≠ 执行顺序，这正是要观察的东西。
	for i := 1; i <= numG; i++ {
		go worker(pr, i, workMS, &wg)
	}

	// main 必须等，否则 main 一返回，整个进程直接退出，goroutine 全被强杀。
	wg.Wait()
	pr.logf("main: all %d goroutines finished", numG)
}

// worker 模拟一个干活的 goroutine。
func worker(pr *printer, id int, workMS int, wg *sync.WaitGroup) {
	defer wg.Done()

	// 第一条日志就是"这个 goroutine 第一次拿到 P"的时刻 —— 排队顺序看这里。
	pr.logf("G%d >>> FIRST RUN   (goid=%d)", id, goid())

	for step := 1; step <= 3; step++ {
		time.Sleep(time.Duration(workMS) * time.Millisecond)
		pr.logf("G%d     step %d", id, step)
	}

	pr.logf("G%d     finished", id)
}
