package main

import (
	"fmt"
	"runtime"
	"sync"
)

var (
	counter int
	wg      sync.WaitGroup
)

func main() {
	wg.Add(2)
	go incCounter(1)
	go incCounter(2)
	wg.Wait()
	fmt.Println("Final Counter:", counter)
}

func incCounter(id int) {
	defer wg.Done()
	for count := 0; count < 2; count++ {
		value := counter // 读
		// runtime.Gosched() 已移除 —— 看看不主动让出时 race 还会不会出现
		value++         // 改（本地副本）
		counter = value // 写
	}
	_ = runtime.NumGoroutine // 保留 runtime import，方便用户自己加回 Gosched 对比
}
