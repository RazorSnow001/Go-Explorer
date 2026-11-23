// 实验 B：加 sync.Mutex —— 修好竞态
//
// 演示内容：结构与实验 A 完全相同，只是把「读-改-写」三步关进临界区，
// 临界区用 mutex.Lock() + defer mutex.Unlock() 保护，结果稳定为 4。
package main

import (
	"fmt"
	"runtime"
	"sync"
)

// counter 是被两个 goroutine 共享的变量。
var counter int

// wg 用来等待两个 goroutine 都结束。
var wg sync.WaitGroup

// mutex 保护 counter。注意：它必须是包级变量（或结构体字段），
// 零值即可用，不需要显式初始化。
var mutex sync.Mutex

// bumpCounter 是「临界区」：一进入就 Lock，函数返回前一定 Unlock。
//
// 为什么把临界区单独抽成一个函数？
// 因为 defer 是「函数结束时执行」，不是「循环结束时执行」。
// 如果直接在 for 循环里写 mutex.Lock(); defer mutex.Unlock()，
// 解锁会推迟到 incCounter 整个函数结束才发生（每次循环都压一次锁），
// 程序会直接死锁。抽成独立函数后，每次调用都是一次完整的加锁-解锁。
func bumpCounter() {
	mutex.Lock()
	defer mutex.Unlock() // 即使中间 panic，Unlock 也一定会执行

	// ---- 临界区开始：以下代码同一时刻只允许一个 goroutine 进入 ----
	value := counter  // ① 读
	runtime.Gosched() // ⏸ 依然故意让出时间片：另一个 goroutine 只能卡在 Lock 上，进不来
	value++           // ② 改
	counter = value   // ③ 写
	// ---- 临界区结束 ----
}

// incCounter 做 2 次自增，每次都通过临界区完成。
func incCounter() {
	defer wg.Done()
	for i := 0; i < 2; i++ {
		bumpCounter()
	}
}

func main() {
	wg.Add(2)
	go incCounter()
	go incCounter()
	wg.Wait()
	fmt.Printf("Final Counter: %d\n", counter)
}
