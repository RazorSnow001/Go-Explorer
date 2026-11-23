// 实验 C：用 sync/atomic 原子操作 —— 另一种修法
//
// 演示内容：单个计数器不需要一把大锁，用 atomic.AddInt64 就能得到正确结果，
// 并且比 Mutex 更轻量。注意：atomic 只能保护「单个变量」这一个操作，
// 无法表达「一组变量要保持一致」这种不变式（那种情况仍然必须用 Mutex）。
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// counter 是被两个 goroutine 共享的计数器。
// 必须是 int64（atomic 操作要求 64 位对齐的地址）；
// 包级变量天然满足对齐要求。
var counter int64

// wg 用来等待两个 goroutine 都结束。
var wg sync.WaitGroup

// incCounter 做 2 次自增。
//
// atomic.AddInt64(&counter, 1) 是一条不可分割的原子指令：
// 「读出旧值 → 加 1 → 写回」由 CPU 保证只会被一个 goroutine 看到，
// 中间不会插入任何别的 goroutine 的访问。
func incCounter() {
	defer wg.Done()
	for i := 0; i < 2; i++ {
		runtime.Gosched() // ⏸ 故意放大调度窗口
		atomic.AddInt64(&counter, 1)
	}
}

func main() {
	wg.Add(2)
	go incCounter()
	go incCounter()
	wg.Wait()
	// 读取共享变量也要用原子读：atomic.LoadInt64。
	// （此处 wg.Wait() 已经提供了 happens-before 边，普通读也安全，
	//  但只要「读」和「写」可能同时发生，就必须用原子读。）
	fmt.Printf("Final Counter: %d\n", atomic.LoadInt64(&counter))
}
