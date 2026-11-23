# Mutex 教学实验室（mutex-lab）

一组「不加锁 vs 加锁 vs 原子操作」的可运行对照实验。
每个子目录是一个独立的 `package main`，可以单独 `go run` 跑出真实结果。

> 环境：`go1.27.4 linux/amd64`
> 本目录自带 `go.mod`（`module mutexlab`），因此可以和父目录的 `module go-explorer` 互不干扰。

---

## 目录结构

```
mutex-lab/
├── go.mod          # module mutexlab —— 和父目录的 module go-explorer 隔离
├── README.md       # 本文件
├── nolock/main.go  # 实验 A：不加锁 → 制造竞态
├── mutex/main.go   # 实验 B：sync.Mutex → 修好
├── atomic/main.go  # 实验 C：sync/atomic → 另一种修法
└── safe/main.go    # 实验 D：贴近生产的最小安全模板
```

---

## 每个实验在演示什么（一句话）

| 实验 | 一句话说明 |
| --- | --- |
| **A. nolock** | 两个 goroutine 同时对同一个变量做「读-改-写」而不加锁，会发生 **数据竞态（data race）**，结果可能丢失更新。 |
| **B. mutex** | 用 `sync.Mutex` 把「读-改-写」关进**临界区**，竞态消失，结果稳定正确。 |
| **C. atomic** | 单个计数器不需要大锁，用 `atomic.AddInt64` 这种**原子操作**也能得到正确结果。 |
| **D. safe** | 把「状态 + 锁」封装进结构体，对外只暴露加锁的方法，就是生产环境该抄的模板。 |

---

## 实验 A：nolock —— 制造竞态

**演示**：不加锁的共享变量会发生数据竞态，最终值不可信。

### 关键代码（`nolock/main.go`）

```go
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
```

**为什么会错**：`value := counter` 和 `counter = value` 之间不是原子的。
两个 goroutine 都可能读到 `counter == 0`，各自算出 1，再先后写回 `1` —— 自增了两次，结果却只有 1。

### 实测输出

```
$ go run -race ./nolock
==================
WARNING: DATA RACE
Write at 0x00000060d298 by goroutine 7:
  main.incCounter()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:29 +0xa6

Previous read at 0x00000060d298 by goroutine 8:
  main.incCounter()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:26 +0x89

Goroutine 7 (running) created at:
  main.main()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:35 +0x38

Goroutine 8 (finished) created at:
  main.main()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:36 +0x44
==================
Final Counter: 2
Found 1 data race(s)
exit status 66
```

要点解读：

- `WARNING: DATA RACE` —— race detector 抓到了竞态。
- 两处冲突栈帧：`main.go:29`（写 `counter = value`）和 `main.go:26`（读 `value := counter`），
  两个 goroutine 访问同一个地址 `0x00000060d298`，彼此之间没有任何同步（happens-before）关系。
- `Found 1 data race(s)` —— 本次执行共检测到 1 处竞态。
- 退出码 `exit status 66`（`go run` 层面返回 1）—— race 一旦发生，程序视为失败。

> ⚠️ **一个必须讲清楚的事实：结果值是概率性的。**
> 实测 12 次 `go run -race ./nolock`：`Final Counter` 出现 **2 共 9 次、4 共 3 次**；
> 不带 `-race` 跑 10 次则**每次都是 2**。但 **race detector 每次都报 `Found 1 data race(s)`**。
>
> 也就是说：竞态**永远存在**，只是这一次运行有没有恰好让错误值显形，取决于调度时序。
> 「这次跑出来是对的」绝不代表代码是安全的 —— 这正是竞态最危险的地方，也是必须开 `-race` 的原因。

---

## 实验 B：mutex —— 用 sync.Mutex 修好

**演示**：把「读-改-写」三步关进临界区，竞态消失，结果稳定为 4。

### 关键代码（`mutex/main.go`）

```go
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
```

**关键点**：即使临界区里有 `runtime.Gosched()` 主动让出 CPU，另一个 goroutine 也只能**卡在 `Lock()` 上**，
进不来临界区 —— 所以 `Gosched` 不再能制造「读到旧值」的窗口。

### 实测输出

```
$ go run -race ./mutex
Final Counter: 4
```

连续跑 3 次，结果均为 `Final Counter: 4`，**没有任何 race 报告**，退出码 0。

---

## 实验 C：atomic —— 用原子操作修好

**演示**：单个计数器用 `atomic.AddInt64` 也能保证正确，而且比 Mutex 更轻量。

### 关键代码（`atomic/main.go`）

```go
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
```

**什么时候用 atomic、什么时候用 Mutex**：

- `atomic`：只保护**一个变量**的单个操作（计数、标志位、指针替换）。
- `Mutex`：要保护**一组变量**，让它们始终保持一致（不变式）时必须用锁。

### 实测输出

```
$ go run -race ./atomic
Final Counter: 4
```

连续跑 3 次，结果均为 `Final Counter: 4`，**没有任何 race 报告**，退出码 0。

---

## 实验 D：safe —— 贴近生产的最小模板

**演示**：把「状态 + 锁」封装进结构体，10 个 goroutine 各加 100 次，稳定得到 1000。

### 关键代码（`safe/main.go`）

```go
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
```

**为什么 `Value()` 也必须加锁**：`n` 是普通 `int`，不是原子类型。
如果 `Inc` 正在写 `n` 的同时 `Value` 在读 `n`，这次「读一个正在被写的 int」**本身就是一次 data race**。
写和读必须共用同一把锁，才能保证读到的永远是一个完整、一致的值。

### 实测输出

```
$ go run ./safe
Total: 1000
```

```
$ go run -race ./safe
Total: 1000
```

两种方式结果一致：**Total: 1000**，无 race 报告，退出码 0。

---

## 对照表

| 程序 | 是否加锁 | 保护方式 | 预期结果 | 实测结果 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `nolock` | ❌ 不加锁 | 无 | 错误值（≠ 4） | `Final Counter: 2`（12 次里 3 次为 4）+ `Found 1 data race(s)`（**每次必报**） | 竞态必然存在，错误值是否显形靠运气 |
| `mutex` | ✅ 加锁 | `sync.Mutex` + 临界区 | `Final Counter: 4` | `Final Counter: 4`（3/3 次），无 race 报告 | `Gosched` 被挡在临界区外，丢失更新不再发生 |
| `atomic` | ✅ 加锁（另一种） | `atomic.AddInt64` / `LoadInt64` | `Final Counter: 4` | `Final Counter: 4`（3/3 次），无 race 报告 | 原子操作不可分割，比 Mutex 更轻量；但只能保护单个变量 |
| `safe` | ✅ 加锁 | 结构体自带 `sync.Mutex` | `Total: 1000` | `Total: 1000`（3/3 次），无 race 报告 | 生产模板：状态和锁封装在一起，读写共用一把锁 |

---

## Mutex 三条守则

### ① Lock 之后立刻 `defer Unlock()`

```go
c.mu.Lock()
defer c.mu.Unlock() // ← 紧跟在 Lock 下一行，永远不会漏

c.n++
```

`defer` 把解锁动作绑定到「函数返回」而不是「我记得到写」，
这样即使临界区中间 `return`、甚至 `panic`，锁也一定会被释放。

> 反例（写在循环里的 `defer` 是本实验最容易踩的坑）：
> `defer` 是**函数级**的，不是**语句块级**的。
> 在 `for` 循环里写 `mutex.Lock(); defer mutex.Unlock()`，
> 解锁会被推迟到整个函数结束 —— 循环第二轮直接把自己锁死（Go 的 `sync.Mutex` 不可重入）。
> 要在循环里加锁，就把临界区抽成独立函数（见实验 B 的 `bumpCounter`）。

### ② 临界区要尽可能小，别在里面做 IO

锁的粒度越大，并发度越低：临界区里的一秒，就是其他 goroutine 白等的一秒。

**反模式**：把锁持有期间的日志写文件、发 HTTP 请求、调用数据库全塞进临界区。

**更好的做法**：在锁外把需要的数据取出来，锁内只做纯内存的最小计算，
需要一致性保护时用「锁内取值 → 锁外干活 → 锁内写回」的两段式。

```go
c.mu.Lock()
snapshot := c.n      // 只在锁内读一次
c.mu.Unlock()

// 耗时的 IO 放在锁外，不阻塞其他 goroutine
result := doSlowIO(snapshot)

c.mu.Lock()
c.n = snapshot + result // 写回时再加锁
c.mu.Unlock()
```

### ③ 锁保护的是「一组需要保持一致的不变式」

加锁不是为了「让某个变量安全」，而是为了保护**一组变量之间必须成立的关系**。

```go
// 不变式：balance >= 0，且 balance 与 history 长度永远对得上
c.mu.Lock()
defer c.mu.Unlock()

c.balance -= amount       // 改 balance
c.history = append(...)   // 同时改 history
```

这类场景**用 `atomic` 做不到** —— 每次原子操作只能保证「一个变量」不变，
两个变量之间的关联必须靠一把锁（或者把状态合并成一个结构体再整体原子替换）。
选 `atomic` 还是 `Mutex`，判断标准就是这一条。

---

## 如何自己复现

```bash
cd /home/changqi/Go-Explorer/mutex-lab
```

### 逐个跑（推荐，能看到每个实验的完整输出）

```bash
go run -race ./nolock   # 预期：WARNING: DATA RACE + Found 1 data race(s) + Final Counter 偏小
go run -race ./mutex    # 预期：Final Counter: 4，无 race 报告
go run -race ./atomic   # 预期：Final Counter: 4，无 race 报告
go run ./safe           # 预期：Total: 1000
```

### 一次性跑完四个

`go run -race ./...` 在这里**不能用**，因为 `go run` 一次只接受一个 main 包，
而本目录下的 `./...` 匹配到 4 个 main 包。实测报错：

```
$ go run -race ./...
go: pattern ./... matches multiple packages:
	mutexlab/atomic
	mutexlab/mutex
	mutexlab/nolock
	mutexlab/safe
```

所以请用下面两种等价方式：

```bash
# 方式一：一次性编译检查全部（含 race 插桩）+ 逐个运行
go vet ./... && go build -race ./... && \
  for d in nolock mutex atomic safe; do echo "== $d =="; go run -race ./$d; done

# 方式二：只编译检查（不做 race 插桩）
go build ./... && go vet ./...
```

> 提示：`go run -race ./nolock` 在检出竞态后会以**非 0 退出码**结束（`exit status 66`），
> 这是**预期的**行为，不是环境故障。

---

## ⚠️ 一句提醒

> **`go run -race` 没报错，不等于代码绝对安全 —— 它只报告「这一次执行中真实发生过的」竞争。**
>
> race detector 是**动态**的：只有当两个 goroutine 恰好真的访问了同一块内存、
> 且中间没有同步关系时，它才抓得到。某次运行没触发（就像实验 A 的 `Final Counter: 4`），
> 只能说明「这一次运气好」，不能说明「代码是对的」。
>
> 实践建议：
> - 单元测试和 CI 里**始终**加 `-race`（并发相关的测试要跑多次、配合 `-count` 提升覆盖率）；
> - 关键路径改用显式的同步原语（`Mutex`、`atomic`、`chan`）来**证明**正确性，而不是靠 detector 去「发现」；
> - 记住 detector 的两个盲区：**不报死锁**（实验 D 的 `IncBad` 就不会被它抓到）、**不报逻辑错误**（结果对了但算法是错的）。

---

## 附：四条命令的真实输出汇总

```
$ go run -race ./nolock
==================
WARNING: DATA RACE
Write at 0x00000060d298 by goroutine 7:
  main.incCounter()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:29 +0xa6

Previous read at 0x00000060d298 by goroutine 8:
  main.incCounter()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:26 +0x89

Goroutine 7 (running) created at:
  main.main()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:35 +0x38

Goroutine 8 (finished) created at:
  main.main()
      /home/changqi/Go-Explorer/mutex-lab/nolock/main.go:36 +0x44
==================
Final Counter: 2
Found 1 data race(s)
exit status 66

$ go run -race ./mutex
Final Counter: 4

$ go run -race ./atomic
Final Counter: 4

$ go run ./safe
Total: 1000
```
