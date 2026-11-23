// 实验 D：贴近生产的最小安全模板
//
// 演示内容：把「状态 + 锁」封装进结构体，对外只暴露加锁的方法，
// 10 个 goroutine 各加 100 次，最终稳定得到 1000。
package main

import (
	"fmt"
	"sync"
)

// SafeCounter 是一个线程安全的计数器。
//
// 结构体自带锁，而不是用包级变量 + 包级锁：
// 这样每个计数器实例彼此独立，互不干扰。
type SafeCounter struct {
	mu sync.Mutex     // 保护 n 的互斥锁
	n  int            // 被 mu 保护的共享状态
	wg sync.WaitGroup // 用于等待一组 goroutine 完成（不是保护 n 的）
}

// Inc 把计数器加 1。
//
// 守则①：Lock 之后立刻写 defer Unlock()。
func (c *SafeCounter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock() // 无论后面怎么退出（哪怕 panic），锁一定会被释放

	c.n++
}

// Value 返回当前计数。
//
// 注意：读也要加锁！
// 因为 Inc 正在写 n 的时候，Value 可能同时在读 n，
// 而「读一个正在被写的 int」本身就是一次 data race。
// 读和写必须共用同一把锁，才能保证「读到的永远是一个完整、一致的值」。
func (c *SafeCounter) Value() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.n
}

// ⚠️ 错误示范（故意不写进可编译代码里，否则会死锁）：
//
//	func (c *SafeCounter) IncBad() {
//	    c.mu.Lock()
//	    c.n++
//	    // 忘了 c.mu.Unlock()！
//	}
//
// 后果：第一次调用 IncBad 后锁永远不会被释放，
// 第二个调用它的 goroutine 会永远阻塞在 Lock() 上，
// wg.Wait() 永远等不到 —— 整个程序「卡死」，而不是报错。
// 这就是为什么必须写 defer c.mu.Unlock()：
// 让解锁跟着「函数返回」自动发生，而不是指望自己记得写。
//
// 另外注意：即使不 panic，忘记 Unlock 也不会让 go run -race 报错，
// race detector 只报「数据竞态」，不报「死锁」——
// 死锁要靠仔细设计 + `go vet` + 运行时超时来发现。

const (
	goroutines   = 10  // 并发 goroutine 数量
	perGoroutine = 100 // 每个 goroutine 自增次数
)

func main() {
	counter := &SafeCounter{}

	for i := 0; i < goroutines; i++ {
		counter.wg.Add(1) // Add 必须在 go 之前调用
		go func() {
			defer counter.wg.Done()
			for j := 0; j < perGoroutine; j++ {
				counter.Inc()
			}
		}()
	}

	counter.wg.Wait() // 等所有 goroutine 跑完
	fmt.Printf("Total: %d\n", counter.Value())
}
