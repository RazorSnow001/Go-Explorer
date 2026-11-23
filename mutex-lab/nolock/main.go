// 实验 A：不加锁 —— 制造数据竞态（data race）
//
// 演示内容：两个 goroutine 同时对同一个全局变量做「读-改-写」，
// 没有锁保护时最终结果小于期望值 4，并会被 go run -race 报告出来。
package main

import (
	"fmt"
	"runtime"
	"sync"
)

// counter 是被两个 goroutine 共享的变量，没有任何保护。
var counter int

// wg 用来等待两个 goroutine 都结束。
var wg sync.WaitGroup

// incCounter 做 2 次自增，每次都是「读-改-写」三步。
//
// 关键：这步不是原子操作，中间还有 runtime.Gosched() 让出时间片，
// 所以两个 goroutine 极可能读到同一个旧值，然后互相覆盖 → 丢失更新。
func incCounter() {
	defer wg.Done()
	for i := 0; i < 2; i++ {
		value := counter  // ① 读
		runtime.Gosched() // ⏸ 故意放大竞态窗口：把 CPU 让给另一个 goroutine
		value++           // ② 改（改的是本地副本）
		counter = value   // ③ 写（覆盖别人可能已经写进去的值）
	}
}

func main() {
	wg.Add(2)
	go incCounter()
	go incCounter()
	wg.Wait() // 等两个 goroutine 都跑完（Wait 提供了 happens-before 边，所以这里的读是安全的）
	fmt.Printf("Final Counter: %d\n", counter)
}
