# 《Go in Action》第 6.2 节（Goroutines）动手实验环境

本目录是一个**独立的 Go module**（自带 `go.mod`），把《Go in Action》第 6 章 6.2 节
"Goroutines" 的三个示例程序原样搬进来，可以从零跑通、可以一行行改、可以立刻看到调度器行为的变化。

目录里没有魔法：没有 vendor、没有第三方依赖、没有 Makefile，`go run listing01.go` 就完事。

---

## 1. 环境信息

### 1.0 目录结构

本项目已经从 mindgraph 仓库里独立出来，现在是一个**仓库外的扁平目录**
（没有中间层级，也没有子文件夹）：

```
/home/changqi/Go-Explorer/
├── README.md        本文件
├── go.mod           module go-explorer
├── .gitignore       忽略 go build ./... 生成的二进制
├── doc.go           本目录的 main 入口(打印用法清单)
├── listing01.go     Listing 6.1  GOMAXPROCS(1)
├── listing04.go     Listing 6.4  GOMAXPROCS(1)，质数
└── listing07.go     Listing 6.7  GOMAXPROCS(2)
```

### 1.1 Go 版本（真实 `go version` 输出）

```
$ go version
go version go1.27.4 linux/amd64
```

### 1.2 module 声明

`go.mod`：

```
module go-explorer

go 1.21
```

（`go 1.21` 是模块的语言版本声明，跟本机装的 1.27.4 工具链不冲突。）

### 1.3 怎么跑

```bash
cd ~/Go-Explorer

gofmt -l .          # 列出格式不合规的文件（空 = 全合规）
go vet ./...        # 静态检查
go build ./...      # 编译整个 module
go run .            # 打印用法清单（本目录的 main 入口）
go run listing01.go # 跑 Listing 6.1
go run listing07.go # 跑 Listing 6.7（GOMAXPROCS=2）
go run listing04.go # 跑 Listing 6.4（质数，输出很长）
go run -race listing01.go   # 竞态检测器
```

> **为什么三个示例都带 `//go:build ignore`？**
> 三个文件各自都声明了 `func main`，放在同一个包里会直接编译失败：
> `main redeclared in this block`。Go 的惯例就是给"单文件示例"加 `//go:build ignore`。
> **关键点：显式写在命令行上的文件不受构建约束限制**，所以
> `go run listing01.go` / `go vet listing01.go` 照常工作，
> 而 `go build ./...` / `go vet ./...` 会跳过它们、只编译 `doc.go`。
> 示例正文一个字没动，只多了一行构建约束。

`doc.go` 是这个目录的 `main` 入口（打印用法清单），也是 `go build ./...` 唯一的编译单元。
> `go build ./...` 会在本目录留下一个 `go-explorer` 可执行文件 —— `.gitignore` 已经把它忽略了，
> 随时可以 `rm -f go-explorer`。

### 1.4 文件 ↔ 书里 Listing ↔ 书内页码 对照

| 本目录文件 | 书上 Listing | 书内页码 | 说明 |
|---|---|---|---|
| `listing01.go` | **Listing 6.1** | **132–133** | 两个 goroutine 轮流打印大小写字母，`GOMAXPROCS(1)` |
| （`listing01.go` 的输出） | Listing 6.2 | 133 | 书里给的"并发"输出 |
| `listing04.go` | **Listing 6.4** | **135–136** | 两个 goroutine 找 5000 以内的质数，`GOMAXPROCS(1)`，观察**时间片切换** |
| （`listing04.go` 的输出） | Listing 6.5 | 136–137 | 书里标了 `** Goroutines Swapped` 的切换点 |
| —（书里的片段） | Listing 6.6 | 137 | `runtime.GOMAXPROCS(runtime.NumCPU())` 的写法 |
| `listing07.go` | **Listing 6.7** | **138** | 同 Listing 6.1，但 `GOMAXPROCS(2)`，观察**并行** |
| `doc.go` | — | — | 本目录的 `main` 入口（打印用法清单），也是 `go build ./...` 的唯一编译单元 |
| `README.md` | — | — | 本文件 |
| `.gitignore` | — | — | 忽略 `go build ./...` 生成的二进制 |

补充阅读位置：第 6 章开篇在书内 **128 页**，6.2 小节标题在 **132 页**，
「Race conditions」小节在 **139 页**（讲的是数据竞争，和 Listing 6.7 的并行行为是两条线）。

### 1.5 与书面的排版差异（已核对，只差空格）

任务给的代码里 `fmt.Printf("%c", char)` 不带空格、`fmt.Println("StartGoroutines")` 没有空格；
书上印的是 `fmt.Printf("%c ", char)` 和 `"Start Goroutines"`。**程序逻辑、行号结构、
每一行注释的位置完全一致**，只是输出里字母之间少一个空格、提示语少一个空格。
读代码对行号不受影响。

---

## 2. 代码逐段讲解（面向 Go 新手）

### 2.1 `runtime.GOMAXPROCS(1)` —— 让调度器只有一个"逻辑处理器"

Go 的调度器不是"一个 goroutine 一个线程"。真实模型是：

```
G (goroutine)  ──被调度──>  M (OS 线程)  ──绑定──>  P (逻辑处理器)
```

- **G** = goroutine，轻量级用户态任务，几 KB 栈，创建成本远低于线程。
- **M** = OS 线程，真正干活的。
- **P** = 逻辑处理器（Logical Processor）。**P 的数量 = `GOMAXPROCS` 的值**。
  每个 P 同时只能跑一个 goroutine。

`runtime.GOMAXPROCS(n)` 设置的是 **P 的个数上限**，也就是"调度器手里有几个并发跑 Go 代码的名额"。

- `GOMAXPROCS(1)` → 只有 **1 个名额** → 一次只能跑一个 goroutine。
  这叫**并发（concurrency）**：多个任务轮流使用同一个名额，靠调度器**时间片轮转**切换。
- `GOMAXPROCS(2)` → **2 个名额** → 两个 goroutine 可以**同时**跑在两个不同的 M 上。
  这叫**并行（parallelism）**。

所以 Listing 6.1 和 6.7 是**同一段代码**，唯一的差别就是这个数字。而 6.4 是把"时间片切换"
放大到肉眼可见：找质数很慢，跑着跑着就被换下去了。

> ⚠️ `GOMAXPROCS` 不等于物理核数。默认值是 `runtime.NumCPU()`（= 机器的逻辑 CPU 数）。
> 你可以设成比物理核数更大（多余的名额会空转，只是增加调度开销），
> 也可以设成 1（在多核机器上强制串行，**复现书里的行为就用这个**）。

### 2.2 `var wg sync.WaitGroup` + `wg.Add(2)` —— 给"要等几个"记账

`WaitGroup` 就是一个**倒计数器**，配合同步原语使用：

```go
var wg sync.WaitGroup
wg.Add(2)   // 记账：我要等 2 个 goroutine
```

- `wg.Add(n)`：计数器 `+n`。必须在 `Wait()` **之前**调用。
- `wg.Done()`：计数器 `-1`。每个 goroutine 结束时调一次。
- `wg.Wait()`：**阻塞**当前 goroutine，直到计数器归零。

注意 `Add` 的数量必须**正好等于**实际会调用 `Done` 的次数：
- 少了 → 计数器提前归零 → `Wait()` 提前放行（不等就返回，见实验 3）。
- 多了 → 计数器永远降不到 0 → `Wait()` 永久阻塞（见实验 5、6）。

### 2.3 `go func() { ... }()` —— 起一个 goroutine

```go
go func() {
    // ... 函数体 ...
}()
```

`go` 关键字 + 一个**匿名函数** + 末尾的 `()`（立刻调用它）。
`go` 的语义是：**把这个函数调用丢进调度器的就绪队列，当前 goroutine 立刻往下走**。
它不会等函数体执行完 —— 这就是"异步"的来源。

> 对比 Listing 6.4：那里不用匿名函数，直接 `go printPrime("A")`。
> 函数名后面**不写括号**表示"把这个函数当 goroutine 跑"；
> 写了括号 `printPrime("A")` 就是普通调用，会同步阻塞。
> 两种写法等价，`go` 后面的那个"函数调用"才变成 goroutine。

### 2.4 `defer wg.Done()` —— 为什么必须用 `defer`

```go
go func() {
    defer wg.Done()   // ← 函数体第一行
    // ... 干活的 ...
}()
```

`defer` 把这次调用**推迟到当前函数返回时**执行**。所以不管函数体里怎么走、
是正常跑完还是中途 `return`（甚至 panic，panic 展开时 defer 仍会跑），
`wg.Done()` **一定会被调用一次**。

如果不用 `defer`，你就得在函数体**最后一行**手写 `wg.Done()`：

```go
go func() {
    for ... { ... }
    wg.Done()   // 万一中间 return / panic，就永远不会被调用 → 计数器卡住
}()
```

用 `defer` 有两个好处：
1. **不会漏**（漏了 → `Wait()` 永久阻塞）；
2. **不会多**（多调一次 → 计数器变负 → `panic: sync: negative WaitGroup counter`）。

而且必须写在**函数体开头**。写在循环里就会每次迭代都 `Done` 一次，计数器飞快地变负。

### 2.5 `wg.Wait()` —— main 必须等

```go
fmt.Println("WaitingToFinish")
wg.Wait()
fmt.Println("\nTerminatingProgram")
```

**Go 的 `main` 函数一返回，整个进程就立刻退出，所有还在跑的 goroutine 被强杀** ——
不管它们干到哪一步。所以如果 main 不等，goroutine 里 `fmt.Printf` 打印的字母
可能一个都看不到，或者只看到一半，程序就"莫名其妙"结束了。

`wg.Wait()` 的作用就是：**把"main 退出"这件事推迟到两个 goroutine 都干完活之后**。

### 2.6 两个 goroutine 共享一个逻辑处理器时会发生什么

（Listing 6.1 + Listing 6.4 的核心）

1. `go` 把两个 goroutine 放进就绪队列；main 自己也在一个 P 上跑，先打印 `WaitingToFinish`，然后卡在 `wg.Wait()`。
2. main 卡在 `Wait()` 上 → P 空出来 → 调度器取出一个 goroutine（比如 B）跑。
3. B 跑一阵（一个**时间片**，通常是 10ms 量级）还没结束 → 被换下，放回就绪队列尾部 → 取 A 跑。
4. A 跑一个时间片 → 换 B ……如此轮转，直到某个 goroutine 跑完。
5. `wg.Wait()` 的计数器归零 → main 解除阻塞 → 打印 `TerminatingProgram` → 进程退出。

**注意**：goroutine 之间没有"谁先谁后"的保证。
`go` 语句的先后 ≠ 执行的先后；即使 B 后启动，也可能先拿到 P。
你只能依赖"最终都会跑完"（配合 `WaitGroup`），**不能依赖顺序**。

### 2.7 `fmt.Printf` 的并发行为

`fmt` 包内部有锁，所以**每一次 `fmt.Printf` 调用**是原子的 —— 不会出现"半个字符串"被劈开。
但 Listing 6.1 里每次只打印**一个字符**（`"%c"`），于是"原子的一字一次"就表现成了
**逐字符交错**：大写字母和小写字母可以一字一顿地混在一起。

（Listing 6.4 用的是 `fmt.Printf("%s:%d\n", ...)`，一整行是原子的，
所以交错只发生在**行与行之间**，不会出现半行撕裂。）

---

## 3. 动手实验清单（重点）

> 每条都是"改哪一行 → 预期看到什么"。所有"预期"都是在本机（go1.27.4 / linux-amd64）
> **真跑出来**的结果，不是推测。
> 建议每条实验都在**副本**上做，别把 `Go-Explorer/` 里的原版改坏：
> `cp listing01.go /tmp/exp.go && cd /tmp && go run exp.go`

---

### 实验 1：把 `GOMAXPROCS(1)` 改成 `GOMAXPROCS(2)`，看输出怎么变

- **改哪一行**：`listing01.go` 第 16 行 `runtime.GOMAXPROCS(1)` → `runtime.GOMAXPROCS(2)`
- **预期看到什么**：输出**不再稳定**。本机实测 5 次：
  - 3 次和 `GOMAXPROCS(1)` 一模一样（全大写在前，全小写在后）
  - 2 次出现**行内逐字符交错**，例如第 3 次：

    ```
    ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZABCdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz
    ```

    第 5 次更明显，**中间就断了又接上**：

    ```
    abcdefghijklmnoABCDEFGHIJKLMpqrstuvwxyzNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz
    ```
- **为什么**：两个 P 名额 → 两个 goroutine 可以同时在不同 M 上跑 → 真并行。
  谁先谁后、每次切在哪，取决于 OS 线程调度，**每次跑都可能不同**。

> 想稳定复现书里 6.7 的"干净"输出，用 `GOMAXPROCS=1`；
> 想看交错，多跑几次。

---

### 实验 2：注释掉 `wg.Wait()`

- **改哪一行**：`listing01.go` 第 53 行 `wg.Wait()` → `// wg.Wait()`
- **预期看到什么**：**程序秒退**，一个字母都打不出来。实测 3 次，输出全部是：

  ```
  StartGoroutines
  WaitingToFinish

  TerminatingProgram
  ```

  （`WaitingToFinish` 和 `TerminatingProgram` 之间那个空行，是 `"\nTerminatingProgram"` 里的 `\n`。）
- **为什么**：main 不等了 → `TerminatingProgram` 打完 → main 返回 → **进程立刻退出**，
  两个 goroutine 还没轮到（或只打了一部分）就被强杀。
  偶尔能抢到一点时间戳，会看到"只打了半截字母"，因为**这是竞态**，
  本机 3 次是全空，但换个机器/加个 `time.Sleep` 就可能看到半截。

---

### 实验 3：删掉 `defer wg.Done()`

- **改哪一行**：`listing01.go` 里两个 `defer wg.Done()` 都删掉（或只删一个）
- **预期看到什么**：**卡死 → 被 Go 的死锁检测器抓出来**。实测输出：

  ```
  StartGoroutines
  WaitingToFinish
  ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz
  fatal error: all goroutines are asleep - deadlock!

  goroutine 1 [sync.WaitGroup.Wait]:
  sync.runtime_SemacquireWaitGroup(0x10?, 0x0?)
  	/snap/go/11411/src/runtime/sema.go:114 +0x2e
  sync.(*WaitGroup).Wait(0x2946d07a1d0)
  	/snap/go/11411/src/sync/waitgroup.go:206 +0x85
  main.main()
  	/tmp/goexp/expA.go:53 +0xd5
  exit status 2
  ```

- **注意（重要，跟"想当然"不一样）**：这里**不是** `panic: sync: negative WaitGroup counter`！
  少 `Done` 是**计数器降不到 0**，属于**永久阻塞**（死锁），不是计数为负。
  Go 的运行时发现"所有 goroutine 都在睡觉且没人能唤醒它们"，就主动报死锁并退出。

- **想真的看到 `negative WaitGroup counter`**，得让 `Done()` 比 `Add()` 多。确定性最小复现：

  ```go
  var wg sync.WaitGroup
  wg.Add(1)
  wg.Done()
  wg.Done()   // ← 多了一次，计数器变成 -1
  ```

  实测输出：

  ```
  Add(1) 完成
  第一次 Done 完成
  panic: sync: negative WaitGroup counter

  goroutine 1 [running]:
  sync.(*WaitGroup).Add(0x12a974e6c1c0, 0xffffffffffffffff)
  ```

  （顺带一提：直接把 Listing 6.1 的 `wg.Add(2)` 改成 `wg.Add(1)` 想触发它，**不一定能复现** ——
  实测 `Add(1)` + 两个 goroutine 各 `Done` 一次，进程在第二次 `Done` 之前就退出了，没有 panic。
  这本身就是"竞态"的活教材。）

---

### 实验 4：开竞态检测器

- **改哪一行**：**不用改代码**，加个参数：`go run -race listing01.go`
- **预期看到什么**：**正常退出，`0` 条竞态报告**。实测：

  ```
  $ go run -race listing01.go
  StartGoroutines
  WaitingToFinish
  ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz
  TerminatingProgram
  $ echo $?
  0
  $ go run -race listing01.go 2>&1 | grep -c "WARNING: DATA RACE"
  0
  ```

- **为什么 0 条**：这两个 goroutine 之间**没有共享内存**（一个写 stdout、一个写 stdout，
  `fmt` 内部自带锁），`WaitGroup` 的计数器由 runtime 用原子操作保护。
  没有共享可变状态 → 没有数据竞争。
- **注意**：`-race` 会**改变时序**，实测这次跑的输出顺序和大写在前那版**反过来了**
  （小写先打）。所以加了 `-race` 之后输出顺序跟之前不一样是正常的，别当成 bug。
- **真正的数据竞争长什么样**：两个 goroutine 并发 `count++` 同一个变量，
  就会打出 `WARNING: DATA RACE` + 两段冲突的栈。

---

### 实验 5：`listing04.go` 改成 `GOMAXPROCS(2)`，看时间片切换变成什么

- **改哪一行**：`listing04.go` 第 19 行 `runtime.GOMAXPROCS(1)` → `runtime.GOMAXPROCS(2)`
- **预期看到什么**：**A/B 两个前缀不再是一整块一整块，而是逐行互相插队**。
  实测（同一次运行）：

  - `GOMAXPROCS(1)`：前缀切换 **1～3 次**（每次跑不一样，实测 6 次的分布见附录第 5 节），
    典型形态是"整块输出"：
    第 3–672 行全是 `B:...`（以 `Completed B` 收尾），
    第 673–1342 行全是 `A:...`（以 `Completed A` 收尾），最后 `TerminatingProgram`。
  - `GOMAXPROCS(2)`：前缀切换 **853 次**。开头几行还是 B 先跑，但很快就插队进来：

    ```
    切换 #1  行 68 : B -> A   (A:2)
    切换 #2  行 120: A -> B   (B:317)
    切换 #3  行 141: B -> A   (A:241)
    切换 #4  行 166: A -> B   (B:449)
    ...
    切换 #7  行 203: B -> A   (A:509)
    切换 #8  行 204: A -> B   (B:571)
    切换 #9  行 219: B -> A   (A:521)
    切换 #10 行 220: A -> B   (B:659)
    ```

    从第 200 行往后基本变成**一行 A、一行 B** 的密集交替。
    `Completed B` 提前到第 1333 行（`GOMAXPROCS=1` 时是 672 行），
    因为两个 goroutine 真正同时在跑，B 不用等 A 让出时间片。

- **怎么自己数切换次数**：

  ```bash
  go run listing04.go | awk '{p=substr($0,1,1)} \
    p=="A"||p=="B" {if(last!="" && p!=last){sw++}; last=p} \
    END{print "前缀切换次数: " sw+0}'
  ```

---

### 实验 5b：把范围改大，逼出"真正的时间片抢占"

- **改哪一行**：`listing04.go` 第 42 行 `outer < 5000` → `outer < 20000`
- **预期看到什么**：本机实测前缀切换从 **1 次变成 15 次**，
  第一次换手在**第 633 行**（B → A），这才是书里 Listing 6.5 标注
  `** Goroutines Swapped` 的那种"来回换手"。改成 `outer < 50000` → **81 次**。
  （改成 500000 就别试了，本机 120 秒都没跑完。）
- **为什么**：`GOMAXPROCS(1)` 下 B 的活儿本来**一个时间片内就跑完了**，
  所以没机会被抢占。活儿变长 → 时间片用完 → 调度器必须换人。

---

### 实验 6：`listing04.go` 删掉一个 goroutine，但 `wg.Add(2)` 不动

- **改哪一行**：删掉 `listing04.go` 第 27 行 `go printPrime("B")`，**`wg.Add(2)` 保持不变**
- **预期看到什么**：**卡在 `wg.Wait()` 上不动**。实测输出：

  ```
  CreateGoroutines
  WaitingToFinish
  A:2
  A:3
  A:5
  A:7
  ...（A 全部打完）
  fatal error: all goroutines are asleep - deadlock!

  goroutine 1 [sync.WaitGroup.Wait]:
  sync.runtime_SemacquireWaitGroup(0x10?, 0x0?)
  ...
  exit status 2
  ```

- **为什么**：计数器 `Add(2)` = 2，只有 A 调用了一次 `Done()` → 计数器停在 1 → 永远等不到 0。
  注意这里**不会**"白等"到天荒地老：Go 运行时的死锁检测会发现
  "所有 goroutine 都处于睡眠状态且无法被唤醒"，立刻报错退出。
- **对照**：如果同时把 `wg.Add(2)` 改成 `wg.Add(1)`，程序就正常结束了 —— 这说明
  **`Add` 的数字必须和实际 `Done` 的次数严格相等**。

---

### 实验 7：`GOMAXPROCS(1)` → `runtime.GOMAXPROCS(runtime.NumCPU())`

- **改哪一行**：`listing01.go` 第 16 行，改成书上 Listing 6.6 的写法：

  ```go
  runtime.GOMAXPROCS(runtime.NumCPU())
  ```

- **预期看到什么**：输出和实验 1 的 `GOMAXPROCS(2)` 一样开始出现交错，且**每次跑都不一样**。
  `NumCPU()` 返回本机的逻辑 CPU 数，比手写 2 更"贴合本机"。
- **顺带**：`go env GOMAXPROCS` 可以看当前默认值；`runtime.NumCPU()` 也可以直接打印。

---

### 实验 8：`wg.Add(2)` 改成 `wg.Add(3)`

- **改哪一行**：`listing01.go` 第 21 行 `wg.Add(2)` → `wg.Add(3)`
- **预期看到什么**：**死锁**，和实验 6 的表现一模一样（`fatal error: all goroutines are asleep - deadlock!`，
  栈顶是 `sync.WaitGroup.Wait`）。计数器 3，只被减了 2 次，永远差 1。

---

### 实验 9：把 `wg.Add` 挪到 `wg.Wait()` 后面（★ 反直觉的一条）

- **改哪一行**：把 `wg.Add(2)` 从第 21 行剪贴到 `wg.Wait()` **下面**
- **预期看到什么**：**不 panic，程序"正常"退出，但什么都没等**。实测：

  ```
  StartGoroutines
  WaitingToFinish

  TerminatingProgram
  [exit=0]
  ```

  和实验 2 的输出**一模一样** —— 因为此刻计数器还是 0，
  `wg.Wait()` 看到计数为 0 **立刻返回**，等于没写。
  两个 goroutine 一个字母都没打出来，main 就退出了。
- **这条坑比 panic 更阴险**：`panic` 会告诉你出错了，
  而"Add 写错位置"**静悄悄地**让 `Wait` 失去意义。
  所以记住铁律：**`Add` 必须在 `Wait` 之前**。

> 顺带说明：Go 源码里其实还有一条更严格的运行时检查
> `panic("sync: WaitGroup misuse: Add called concurrently with Wait")`
> （`$GOROOT/src/sync/waitgroup.go`），触发条件是
> **`Add` 与尚未返回的 `Wait` 真的并发发生**。
> 本机专门构造了并发版本尝试 5 次，**一次都没触发** ——
> 它属于窄窗口竞态。**别去赌它会给你报错**，直接写对顺序才是正道。

---

### 实验 10：故意制造一次真正的数据竞争（体会 `-race`）

- **改哪一行**：`listing01.go` 里加一个包级变量 `var counter int`，
  两个 goroutine 里各写 `counter++`（各 1000 次）
- **预期看到什么**：`go run -race listing01.go` 会打出（实测原文）：

  ```
  ==================
  WARNING: DATA RACE
  Read at 0x000000609098 by goroutine 8:
    main.main.func1()
        /tmp/goexp/exp10.go:17 +0x8d

  Previous write at 0x000000609098 by goroutine 9:
    main.main.func1()
        /tmp/goexp/exp10.go:17 +0xa5

  Goroutine 8 (running) created at:
    main.main()
        /tmp/goexp/exp10.go:14 +0x64
  ```

  读出来是 `Read at ... / Previous write at ...` —— 因为 `counter++` 是
  "读-改-写"三步，检测器报的是**读**和**写**这两步打架。
- **不加 `-race` 呢**：实测连跑 3 次，**每次都算出正确的 `counter = 2000`**，
  **没有任何报错**。这正是数据竞争最可怕的地方：
  **它不一定会崩，也不会一定算错**，只是"某天某台机器上偶尔少加了一次"。
  所以**并发代码必须常开 `-race` 测**，不能靠"我跑了几次都对"来判断。

---

### 实验 11：Goroutine 泄漏的直觉

- **改哪一行**：在 `listing01.go` 里再加一个 `go func() { for { time.Sleep(time.Second) } }()`
- **预期看到什么**：**程序照样正常结束**（main 退出 = 进程退出 = 所有 goroutine 被强杀）。
  但这个直觉只对**命令行程序**成立：
  在**长跑的服务**里，这个 goroutine 会永远活着 → **内存/goroutine 泄漏**。
  `go tool pprof` 或 `runtime.NumGoroutine()` 能看到它一直在。

---

## 4. 常见坑

### 坑 1：`GOMAXPROCS` ≠ 物理核数，也 ≠ 并发 goroutine 数

- `GOMAXPROCS(n)` 设的是 **P 的个数**（同时能跑 Go 代码的名额），不是"能开多少 goroutine"。
  goroutine 想开多少开多少，它们排队等 P。
- 默认值 = `runtime.NumCPU()` = 机器的逻辑 CPU 数。
- 设得比物理核数大 → 多出来的 P 抢不到 M，只是白花开销。
- 设成 1 → 强制串行，**这是复现书里时间片行为的正确姿势**，
  别用"我这台机器核多所以看不到切换"来搪塞。
- 想要真的并行加速 → `GOMAXPROCS(runtime.NumCPU())` **并且确认有 CPU 密集的并行段**。
  书里也说了：**"用了更多逻辑处理器不一定更快，要 benchmark"**。

### 坑 2：`WaitGroup` 的计数必须严格配平

| 错误 | 后果 | 本机实测报错 |
|---|---|---|
| `Done()` 比 `Add()` 多 | 计数器为负 | `panic: sync: negative WaitGroup counter` |
| `Done()` 比 `Add()` 少 | 永久阻塞 | `fatal error: all goroutines are asleep - deadlock!` |
| `Add()` 写在 `Wait()` 之后 | `Wait` 形同虚设（计数器是 0，立刻返回） | **不报错**！程序照常退出，goroutine 被强杀（见实验 9） |

- **`Add` 必须在 `Wait` 之前**；同一个 `wg` 第二次用之前，前一次 `Wait` 必须已经返回。
- **`WaitGroup` 不能被复制**（它内部含 `sync.noCopy`）。`go vet` 能抓出来，实测：

  ```
  $ go vet copylock.go
  copylock.go:11:9: assignment copies lock value to wg2: sync.WaitGroup contains sync.noCopy
  copylock.go:12:14: call of fmt.Println copies lock value: sync.WaitGroup contains sync.noCopy
  ```

  所以别写 `wg2 := wg`、别把 `wg` 按值传参、别把它塞进 struct 之后再整体复制。
- `wg` 应该是**包级变量**（Listing 6.4 就是）还是**函数内局部变量**（Listing 6.1/6.7 就是）
  都行；只要**别复制**。
- `WaitGroup` **不是**用来"传值/回传结果"的，它只管"等结束"。
  要传结果用 `channel` 或 `sync.Map` 之类的。

### 坑 3：`defer wg.Done()` 放错位置

- 必须写在 **goroutine 函数体的第一行**，不能写在循环里、不能写在 `go` 外面。
- 写在 goroutine **外面**（比如 `go func(){...}(); defer wg.Done()`）→
  `Done` 会在 `main` 返回时才执行 → 要么死锁，要么根本没等到。
- 写在循环里 → 计数器飞快地变负 → `panic: sync: negative WaitGroup counter`。

### 坑 4：goroutine 泄漏（goroutine leak）

- goroutine **不会被自动回收**，它只会在两种情况下消失：自己结束，或者整个进程退出。
- 在服务里，只要有一个 `go` 出去的任务**永远不返回**（卡在 `select{}`、等一个没人发的 channel、
  等一个永远不来的 HTTP 响应），它就**永远占着栈和内存**。
- 典型症状：跑久了内存一路涨、`runtime.NumGoroutine()` 单调上升。
- 排查：`pprof` 的 goroutine profile、或者在代码里定期打印 `runtime.NumGoroutine()`。
- **注意**：命令行程序退出时进程会带走所有 goroutine，所以**本地测试看不出泄漏**，
  必须放到长跑环境里观察。

### 坑 5：以为"goroutine 的执行顺序 = `go` 语句的书写顺序"

不成立。`go` 只是"入队"，谁先跑取决于调度器。
`listing01.go` 实测 5 次全是**大写先打**，但那只是 `GOMAXPROCS(1)` +
`runnext` 槽位的巧合；换台机器、换个 Go 版本、加个 `GOMAXPROCS(2)`，
顺序就变（实验 1 的第 5 次甚至从小写开头）。
**永远不要依赖 goroutine 的执行顺序**，只依赖 `WaitGroup` 保证"都跑完了"。

### 坑 6：`fmt.Printf` 每次调用是原子的，但"逐字符"调用会看起来像撕裂

`fmt` 有内部锁，单次 `Printf` 不会被打断。但 Listing 6.1 每次只打一个字符，
所以交错是**字符级**的；Listing 6.4 每次打一整行，所以交错是**行级**的。
看到"半个单词"不用担心，那不是内存损坏。

### 坑 7：三个 `func main` 放在一个目录里

`go build ./...` 会报：

```
main redeclared in this block
```

两个标准解法：
1. **本目录的做法**：给每个示例加 `//go:build ignore`，另放一个 `doc.go` 当包锚点。
   显式命名的文件不受约束限制，`go run listing01.go` 照常。
2. 每个示例放**各自的子目录**（各成一个 package）——目录多了点，但结构更常规。

---

## 5. 附录：`listing04.go` 的真实输出节选

`GOMAXPROCS(1)` 下每次跑都是 **1343 行**（两个 goroutine 各打印 669 个质数 + 1 行 `Completed`）。
下面是其中一次运行的完整节选；**前缀交错点每次跑都不一样**，本节末尾有 6 次运行的统计。

### 前 30 行

```
CreateGoroutines
WaitingToFinish
B:2
B:3
B:5
B:7
B:11
B:13
B:17
B:19
B:23
B:29
B:31
B:37
B:41
B:43
B:47
B:53
B:59
B:61
B:67
B:71
B:73
B:79
B:83
B:89
B:97
B:101
B:103
B:107
```

### 后 30 行

```
A:4783
A:4787
A:4789
A:4793
A:4799
A:4801
A:4813
A:4817
A:4831
A:4861
A:4871
A:4877
A:4889
A:4903
A:4909
A:4919
A:4931
A:4933
A:4937
A:4943
A:4951
A:4957
A:4967
A:4969
A:4973
A:4987
A:4993
A:4999
Completed A
TerminatingProgram
```

### `Completed` 两行的位置（**注意：每次跑都可能不一样**）

上面这份节选来自"1 次切换"的那次运行：

```
672:Completed B
1342:Completed A
```

但**这不是固定结果**。实测连跑 6 次，统计如下：

| 次数 | A/B 前缀切换次数 | 首行前缀 | `Completed A` 在第几行 | `Completed B` 在第几行 |
|---|---|---|---|---|
| run1 | 2 | B | 1293 | 1342 |
| run2 | 2 | B | 1285 | 1342 |
| run3 | 3 | B | 1342 | 1304 |
| run4 | 2 | B | 1285 | 1342 |
| run5 | **1** | B | 1342 | 672 |
| run6 | **1** | B | 1342 | 672 |

**切换次数在 1～3 之间飘，谁先跑完也会翻。** 这正是书里那句话的实证：
*"Every time you run this program, the scheduler will slightly change the point
where the time slice occurs."*（每次跑，切片点都会挪一点）

### ★ A / B 前缀的交错点在哪里

**交错点不固定，取决于那次运行落在哪个时间片上。** 两种典型形态：

**形态一（1 次切换，上面那份节选）** —— B 一个时间片内跑完了全程，A 才开始：

```
669: B:4987
670: B:4993
671: B:4999
672: Completed B      ← B 干完了
673: A:2              ← ★ 交错点：B → A
674: A:3
675: A:5
```

**形态二（2 次切换，实测另一次运行的第 626–632 行 / 1295–1301 行）** ——
B 跑到一半**被时间片抢占**，A 接上去一口气跑完，B 再回来接着跑：

```
626: B:6149
627: B:6151
628: A:2              ← ★ 第 1 次交错：B → A（B 被抢占）
...
1296: A:4993
1297: A:4999
1298: Completed A
1299: B:4639          ← ★ 第 2 次交错：A → B（B 回来接着跑它没跑完的部分）
1300: B:4643
```

**形态二就是书里 Listing 6.5 标注 `** Goroutines Swapped` 的那个场景。**
B 在中途被换下去、之后又切回来 —— 只是本机跑得快，切换点比书里靠后得多。

**想稳定看到大量来回切换，有两条路**（都已实测）：

1. **改 `GOMAXPROCS(2)`** → 前缀切换从 **1～3 次暴增到 853 次**，
   第 68 行就开始插队，第 200 行往后基本是一行 A 一行 B 地交替（见实验 5）。
2. **让每个 goroutine 的活儿变长** —— 把 `outer < 5000` 改成更大的数，
   时间片（10ms 量级）一定会用完 → 必然出现切片抢占。实测：
   - `outer < 20000` → 前缀切换 **15 次**（第 633 行第一次换手，和书里的 "Goroutines Swapped" 一个味道）
   - `outer < 50000` → 前缀切换 **81 次**
   - `outer < 500000` → 太慢（本机跑 120 秒没跑完），别用这个值

---

## 6. 一句话总结

`GOMAXPROCS` 决定**同时能跑几个**，`go` 决定**能起几个**，
`WaitGroup` 决定**什么时候算干完**，而 `main` 决定**什么时候整个进程可以死**。
Listing 6.1 / 6.4 / 6.7 三个例子把这四个旋钮各拧了一个位置，
你把上面 11 个实验挨个跑一遍，Go 调度器在你脑子里就从黑箱变成白箱了。
