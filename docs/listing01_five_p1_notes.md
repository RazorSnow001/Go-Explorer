# listing01_five_p1.go 实验说明

> `listing01_five.go` 的**单逻辑处理器改版**：锁死 1 个 P，隔离掉多 P 的噪音，专心看队列。

---

## 1. 和 `listing01_five.go` 的差别

**一句话：`listing01_five.go` 让调度器自己决定用几个 P，本文件硬锁 `runtime.GOMAXPROCS(1)`，只剩 P0 一个逻辑处理器。**

| | `listing01_five.go` | `listing01_five_p1.go`（本文件） |
|---|---|---|
| GOMAXPROCS | **不设**（只读出来打印），默认 = NumCPU = 8 | **第一行就 `runtime.GOMAXPROCS(1)`** |
| 观察对象 | 多 P 真并行 + work stealing，顺序被搅乱 | **单 P 内部**的入队/出队顺序 |
| 队列预测块 | 无 | **每建一个 goroutine 就打印一段手写的队列状态预测** |
| 自检 | 无 | **结尾自动比对「预测顺序 vs 实际顺序」并给出 ✅/❌** |
| G3 重活 | 永远重 | **`-heavy` 开关**（默认 true，可关成轻量） |
| 记录执行顺序 | 无 | `sync.Mutex` + `sync.Once` 保护 `[]string`，记录**每个 G 第一次打印**的先后 |

为什么要改：多 P 的时候，`runnext` 是 **per-P** 的，其他 P 还会 **steal** 任务，
5 个 goroutine 会被分头扔到 8 个 P 上并行执行 —— 环形队列的 FIFO 顺序**根本观察不到**。
锁死 1 个 P 之后，队列就成了唯一变量。

---

## 2. 运行环境

| 项 | 值 |
|---|---|
| Go | `go version go1.27.4 linux/amd64` |
| 平台 | `linux/amd64`（WSL2，服务器 razor-wsl2） |
| CPU 逻辑核 | 8（`runtime.NumCPU() == 8`） |
| 目录 | `/home/changqi/Go-Explorer/` |

```bash
cd /home/changqi/Go-Explorer

go run listing01_five_p1.go              # 默认 heavy，G3 做 CPU 密集计算
go run listing01_five_p1.go -heavy=false # G3 只打一行, 纯看队列顺序
```

> **模块模式没有遇到任何问题**，不需要 `GO111MODULE=off`。
> 文件带 `//go:build ignore`，但**显式写在命令行上的文件豁免构建约束**，
> 所以 `go run listing01_five_p1.go` / `go vet listing01_five_p1.go` 都正常；
> `go build ./...` / `go vet ./...` 会跳过它，不会和别的 listing 撞 `main redeclared`。

---

## 3. 程序设计说明

### 3.1 五个 goroutine

| G | 捕获的东西 | 打印内容 | 工作量 | 观察点 |
|---|---|---|---|---|
| **G1** | 外层 `alphabet` 字符串 | `G1 START` 身份行 + 大写字母表 A–Z × 3 轮 | 极轻（3 次 Printf，≈0.01ms） | runnext 整块输出 |
| **G2** | 外层 `numberCount`（=50） | `G2 START` 身份行 + `01`–`50`，每行 10 个 | 中等（5 次 Printf，≈0.02ms） | ring 第 2 位 |
| **G3** | 外层 `sieveLimit`（=20000000） | `G3 START` 身份行 + 结尾一行 `done: primes = 1270607` | **重**：筛素数 ≈140ms（14 个时间片）；`-heavy=false` 时只打一行 | **被抢占的现场** |
| **G4** | 外层 `pattern` + `patternRows`（=40） | `G4 START` 身份行 + `#=+*` 图案 40 行 | 中等（40 次 Printf，≈0.05ms） | **从被抢占的 G3 手里抢走 CPU 的那个** |
| **G5** | **字面量**（不捕获变量） | `G5 START` 身份行 + 中文 + emoji 10 行 | 轻量（10 次 Printf，≈0.02ms） | **最后创建 → runnext VIP 槽 → 第一个跑** |

每个 G 的**第一行都是身份行**（`Gx START — <做什么>`），一眼能认出是哪个 G。
所有行都带 `[Gx +NNN.NNNms]` 前缀（`tag()`，沿用 `listing01_five.go` 的写法）。

### 3.2 队列状态预测块怎么用

每写完一个 `go func(){}()`，main **立刻**打印一段**手写的**队列状态预测：

```
[创建 #5] go func(){G5}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G5      ← 最后创建的抢到 VIP 槽
       local ring (FIFO)      : G1 → G2 → G3 → G4
```

**这些是作者手写的理论值，不是运行时读出来的** —— Go runtime 不把 `runnext` 槽和本地环形队列
暴露给用户代码（没有任何 API 能读它们）。程序里用 `predict()` 函数打印，
每次都标着「预测」两个字，就是为了不让读者误以为是 runtime 的真实状态。

推导依据（对着 `$GOROOT/src/runtime/proc.go` 核过）：

```
newproc → runqput(pp, g, next=true)
                    ↓
   runnext ← 新 G           （VIP 槽，单槽，LIFO）
   旧 runnext → 本地环形队列尾部   （256 格，FIFO）
```

所以建完 G1..G5：

| 创建 | runnext (VIP) | local ring (FIFO) |
|---|---|---|
| #1 G1 | G1 | (空) |
| #2 G2 | G2 | G1 |
| #3 G3 | G3 | G1 → G2 |
| #4 G4 | G4 | G1 → G2 → G3 |
| #5 G5 | **G5** | **G1 → G2 → G3 → G4** |

main 阻塞后，调度器先取 runnext（G5），再按 FIFO 从 ring 头部取（G1→G2→G3→G4）。
**预测执行顺序 = G5 → G1 → G2 → G3 → G4**。

### 3.3 结尾的自检

`orderMu sync.Mutex` 保护一个 `[]string`，记录**每个 G 第一次打印**的先后。
关键细节：**append 和写 stdout 在同一把锁里**（`firstPrint()`），
所以**记录下来的顺序 = 身份行在输出里出现的顺序**，两者严格一致，
阶段 4 的比对才有意义。每个 G 用一个 `sync.Once` 保证只登记一次。

比对的是「**第一次打印**顺序」而不是「完成顺序」——
因为 G3 被抢占后，它的 `done:` 行必然晚于 G4，但它的**第一次打印**（START 行）仍在第 4 位。

### 3.4 `-heavy` 开关

```go
heavy := flag.Bool("heavy", true, "让 G3 做 CPU 密集筛素数 (远超 10ms 时间片), 以观察调度器抢占")
flag.Parse()
```

- 默认（`true`）：G3 筛 2000 万以内素数，≈140ms → 会被抢占十几次。
- `-heavy=false`：G3 只打一行 `G3 START — 轻量模式` 就结束 → **全程零抢占**，
  五个 G 在 0.05ms 内跑完，输出是最纯粹的队列顺序。

`go run listing01_five_p1.go -heavy=false` 可以直接写，不需要 `--`。

---

## 4. 完整输出

### 4.1 普通构建（heavy，默认）—— 原样贴出，未删减

命令：`go run listing01_five_p1.go`

```
--- 阶段 1: 设定 ---
main: runtime.GOMAXPROCS(1) 已锁定 → 实际 P 数 = 1
main: 机器逻辑核 runtime.NumCPU() = 8
main: 本次实验只用 P0 一个逻辑处理器, 隔离掉 work stealing / 核间并行噪音
main: 唯一目标: 看清 runnext VIP 槽 (LIFO) + 本地环形队列 (FIFO) 的入队/出队顺序
main: heavy 模式 = true
main:   G3 会做 CPU 密集筛素数, 远超 10ms 时间片 → 预期能看到抢占

main: created G1 (轻量, 大写字母表 x 3 轮)
[创建 #1] go func(){G1}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G1
       local ring (FIFO)      : (空)
main: created G2 (中等, 数字 01..50, 每行 10 个)
[创建 #2] go func(){G2}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G2      ← 旧 runnext (G1) 被踢进 ring 尾部
       local ring (FIFO)      : G1
main: created G3 (重, CPU 密集筛素数, ~140ms >> 10ms 时间片)
[创建 #3] go func(){G3}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G3      ← 旧 runnext (G2) 被踢进 ring 尾部
       local ring (FIFO)      : G1 → G2
main: created G4 (中等, ASCII 符号图案 #=+*, 40 行)
[创建 #4] go func(){G4}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G4      ← 旧 runnext (G3) 被踢进 ring 尾部
       local ring (FIFO)      : G1 → G2 → G3
main: created G5 (轻量, 中文 + emoji, 10 行) -- 最后创建, 预期第一个跑
[创建 #5] go func(){G5}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G5      ← 最后创建的抢到 VIP 槽
       local ring (FIFO)      : G1 → G2 → G3 → G4

--- 阶段 2: main 阻塞在 wg.Wait(), P0 开始取活 ---
调度器取活顺序: ① runnext (VIP 槽)  →  ② local ring 队头 (FIFO)
预测执行顺序   : G5 → G1 → G2 → G3 → G4
预测第一个打印 : G5

[G5 +    0.283ms]  G5 START — 中文 + emoji
[G5 +    0.295ms] 第 1 行：你好，Go 并发 🐹
[G5 +    0.296ms] 第 2 行：你好，Go 并发 🐹
[G5 +    0.297ms] 第 3 行：你好，Go 并发 🐹
[G5 +    0.298ms] 第 4 行：你好，Go 并发 🐹
[G5 +    0.299ms] 第 5 行：你好，Go 并发 🐹
[G5 +    0.300ms] 第 6 行：你好，Go 并发 🐹
[G5 +    0.301ms] 第 7 行：你好，Go 并发 🐹
[G5 +    0.302ms] 第 8 行：你好，Go 并发 🐹
[G5 +    0.303ms] 第 9 行：你好，Go 并发 🐹
[G5 +    0.304ms] 第 10 行：你好，Go 并发 🐹
[G1 +    0.306ms]  G1 START — 打印大写字母表 (A-Z x 3 轮)
[G1 +    0.310ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )
[G1 +    0.312ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 2 )
[G1 +    0.313ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 3 )
[G2 +    0.314ms]  G2 START — 打印数字 01..50, 每行 10 个
[G2 +    0.320ms]  01 02 03 04 05 06 07 08 09 10 
[G2 +    0.323ms]  11 12 13 14 15 16 17 18 19 20 
[G2 +    0.325ms]  21 22 23 24 25 26 27 28 29 30 
[G2 +    0.327ms]  31 32 33 34 35 36 37 38 39 40 
[G2 +    0.329ms]  41 42 43 44 45 46 47 48 49 50 
[G3 +    0.331ms]  G3 START — CPU 密集筛素数 (up to 20000000), 远超 10ms 时间片
[G4 +   15.920ms]  G4 START — 打印 ASCII 符号图案 #=+*
[G4 +   15.954ms] 1  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.956ms] 2  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.958ms] 3  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.959ms] 4  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.960ms] 5  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.961ms] 6  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.962ms] 7  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.962ms] 8  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.963ms] 9  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.964ms] 10 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.965ms] 11 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.966ms] 12 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.967ms] 13 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.968ms] 14 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.969ms] 15 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.970ms] 16 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.971ms] 17 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.972ms] 18 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.973ms] 19 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.974ms] 20 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.975ms] 21 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.976ms] 22 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.977ms] 23 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.987ms] 24 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.989ms] 25 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.990ms] 26 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.991ms] 27 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.992ms] 28 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.993ms] 29 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.994ms] 30 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.995ms] 31 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.997ms] 32 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.998ms] 33 #=+*#=+*#=+*#=+*#=+*
[G4 +   16.000ms] 34 #=+*#=+*#=+*#=+*#=+*
[G4 +   16.002ms] 35 #=+*#=+*#=+*#=+*#=+*
[G4 +   16.003ms] 36 #=+*#=+*#=+*#=+*#=+*
[G4 +   16.005ms] 37 #=+*#=+*#=+*#=+*#=+*
[G4 +   16.007ms] 38 #=+*#=+*#=+*#=+*#=+*
[G4 +   16.008ms] 39 #=+*#=+*#=+*#=+*#=+*
[G4 +   16.010ms] 40 #=+*#=+*#=+*#=+*#=+*
[G3 +  136.299ms]  done: primes = 1270607

--- 阶段 3: wg.Wait() 返回, 5 个 goroutine 全部结束 ---
main: 重点检查 → G3 的 done 行是否排在 G4 的 40 行之后 (被抢占的证据)

--- 阶段 4: 验证 ---
预测的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
实际的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
结论: ✅ 一致 —— 单 P 下 runnext(LIFO) + ring(FIFO) 的预测成立

TerminatingProgram
```
### 4.2 `-heavy=false`（G3 轻量，全程零抢占）—— 原样贴出，未删减

命令：`go run listing01_five_p1.go -heavy=false`

```
--- 阶段 1: 设定 ---
main: runtime.GOMAXPROCS(1) 已锁定 → 实际 P 数 = 1
main: 机器逻辑核 runtime.NumCPU() = 8
main: 本次实验只用 P0 一个逻辑处理器, 隔离掉 work stealing / 核间并行噪音
main: 唯一目标: 看清 runnext VIP 槽 (LIFO) + 本地环形队列 (FIFO) 的入队/出队顺序
main: heavy 模式 = false
main:   G3 只打一行就结束 → 本次只看纯队列顺序, 没有任何抢占

main: created G1 (轻量, 大写字母表 x 3 轮)
[创建 #1] go func(){G1}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G1
       local ring (FIFO)      : (空)
main: created G2 (中等, 数字 01..50, 每行 10 个)
[创建 #2] go func(){G2}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G2      ← 旧 runnext (G1) 被踢进 ring 尾部
       local ring (FIFO)      : G1
main: created G3 (重, CPU 密集筛素数, ~140ms >> 10ms 时间片)
[创建 #3] go func(){G3}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G3      ← 旧 runnext (G2) 被踢进 ring 尾部
       local ring (FIFO)      : G1 → G2
main: created G4 (中等, ASCII 符号图案 #=+*, 40 行)
[创建 #4] go func(){G4}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G4      ← 旧 runnext (G3) 被踢进 ring 尾部
       local ring (FIFO)      : G1 → G2 → G3
main: created G5 (轻量, 中文 + emoji, 10 行) -- 最后创建, 预期第一个跑
[创建 #5] go func(){G5}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G5      ← 最后创建的抢到 VIP 槽
       local ring (FIFO)      : G1 → G2 → G3 → G4

--- 阶段 2: main 阻塞在 wg.Wait(), P0 开始取活 ---
调度器取活顺序: ① runnext (VIP 槽)  →  ② local ring 队头 (FIFO)
预测执行顺序   : G5 → G1 → G2 → G3 → G4
预测第一个打印 : G5

[G5 +    0.286ms]  G5 START — 中文 + emoji
[G5 +    0.297ms] 第 1 行：你好，Go 并发 🐹
[G5 +    0.298ms] 第 2 行：你好，Go 并发 🐹
[G5 +    0.299ms] 第 3 行：你好，Go 并发 🐹
[G5 +    0.300ms] 第 4 行：你好，Go 并发 🐹
[G5 +    0.301ms] 第 5 行：你好，Go 并发 🐹
[G5 +    0.302ms] 第 6 行：你好，Go 并发 🐹
[G5 +    0.303ms] 第 7 行：你好，Go 并发 🐹
[G5 +    0.304ms] 第 8 行：你好，Go 并发 🐹
[G5 +    0.305ms] 第 9 行：你好，Go 并发 🐹
[G5 +    0.306ms] 第 10 行：你好，Go 并发 🐹
[G1 +    0.308ms]  G1 START — 打印大写字母表 (A-Z x 3 轮)
[G1 +    0.312ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )
[G1 +    0.313ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 2 )
[G1 +    0.314ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 3 )
[G2 +    0.316ms]  G2 START — 打印数字 01..50, 每行 10 个
[G2 +    0.321ms]  01 02 03 04 05 06 07 08 09 10 
[G2 +    0.324ms]  11 12 13 14 15 16 17 18 19 20 
[G2 +    0.326ms]  21 22 23 24 25 26 27 28 29 30 
[G2 +    0.328ms]  31 32 33 34 35 36 37 38 39 40 
[G2 +    0.331ms]  41 42 43 44 45 46 47 48 49 50 
[G3 +    0.332ms]  G3 START — 轻量模式 (-heavy=false), 只打这一行就结束
[G4 +    0.336ms]  G4 START — 打印 ASCII 符号图案 #=+*
[G4 +    0.339ms] 1  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.340ms] 2  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.341ms] 3  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.342ms] 4  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.343ms] 5  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.344ms] 6  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.345ms] 7  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.346ms] 8  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.347ms] 9  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.348ms] 10 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.349ms] 11 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.350ms] 12 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.351ms] 13 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.352ms] 14 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.353ms] 15 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.354ms] 16 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.355ms] 17 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.356ms] 18 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.357ms] 19 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.358ms] 20 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.359ms] 21 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.360ms] 22 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.361ms] 23 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.367ms] 24 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.368ms] 25 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.369ms] 26 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.370ms] 27 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.371ms] 28 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.372ms] 29 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.373ms] 30 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.374ms] 31 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.375ms] 32 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.376ms] 33 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.377ms] 34 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.378ms] 35 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.379ms] 36 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.380ms] 37 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.381ms] 38 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.382ms] 39 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.383ms] 40 #=+*#=+*#=+*#=+*#=+*

--- 阶段 3: wg.Wait() 返回, 5 个 goroutine 全部结束 ---
main: 重点检查 → G5 的 START 行是否紧跟在 main: created G5 之后 (runnext 抢到槽的证据)

--- 阶段 4: 验证 ---
预测的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
实际的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
结论: ✅ 一致 —— 单 P 下 runnext(LIFO) + ring(FIFO) 的预测成立

TerminatingProgram
```

---

## 5. 「怎么读这份输出」

### 5.1 第一步：看阶段 1 的横幅 —— 确认实验设定没跑偏

```
--- 阶段 1: 设定 ---
main: runtime.GOMAXPROCS(1) 已锁定 → 实际 P 数 = 1
main: 机器逻辑核 runtime.NumCPU() = 8
```

**这两行是前提**：机器有 8 个逻辑核，但本次实验**只用了 1 个逻辑处理器（P0）**。
`runtime.GOMAXPROCS(1)` 是 `main` 的第一条语句，早于任何 `go` 语句，
所以五个 goroutine 从头到尾只在一个 P 上排队。

### 5.2 第二步：看五个预测块 —— 这是「手写理论」

```
[创建 #1] go func(){G1}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G1
       local ring (FIFO)      : (空)
...
[创建 #5] go func(){G5}
   预测 P0 队列状态 →
       runnext (VIP 槽, LIFO) : G5      ← 最后创建的抢到 VIP 槽
       local ring (FIFO)      : G1 → G2 → G3 → G4
```

**这五段是「预测」，不是实测。** 记住最终状态：runnext = G5，ring = G1→G2→G3→G4。
接下来看实际输出对不对得上。

> 注意 G2 那段开始出现的注释「旧 runnext (G1) 被踢进 ring 尾部」——
> 这就是 **LIFO 的来源**：每次新 G 挤进 VIP 槽，被挤下来的那个进 ring **尾部**。
> 所以**最早创建的 G1 反而排在 ring 最前面**（FIFO），**最后创建的 G5 反而最先跑**。

### 5.3 第三步：看阶段 2 之后的实际输出 —— 三个机制逐条对号入座

**① runnext 抢到槽的证据（LIFO）**

```
[G5 +    0.283ms]  G5 START — 中文 + emoji
```

**第一个出声的是 G5** —— 它是**最后创建**的那个（`main: created G5` 排在所有创建日志的最后）。
创建顺序和执行顺序**正好相反**，这就是 VIP 槽的 LIFO。

> 对照 `listing01.go`：那里也是第二个 `go func()`（大写）先跑，同一个机制。

**② 环形队列 FIFO 的证据**

```
[G5 +    0.283ms]  G5 START — 中文 + emoji          ← runnext 用掉了
[G1 +    0.306ms]  G1 START — 打印大写字母表            ← ring 队头
[G2 +    0.314ms]  G2 START — 打印数字 01..50            ← ring 第 2
[G3 +    0.331ms]  G3 START — CPU 密集筛素数...           ← ring 第 3
```

**G1 → G2 → G3 严格按创建顺序**，因为 G1 当初被 G2 挤出 VIP 槽时落到了 ring **队头**，
而 ring 是 **FIFO**（256 格环形数组，按 `runqhead` 递增取）。

完整顺序 **G5 → G1 → G2 → G3 → G4**，与阶段 2 打印的预测**逐条对上**。

**③ 10ms 时间片抢占的证据**

heavy 模式下看这两行**之间**：

```
[G3 +    0.331ms]  G3 START — CPU 密集筛素数 (up to 20000000), 远超 10ms 时间片
                        ← 这里空了约 15.6ms，一个字符都没打
[G4 +   15.920ms]  G4 START — 打印 ASCII 符号图案 #=+*
[G4 +   16.0ms ~ 16.2ms]  ...共 40 行...
[G3 +  136.299ms]  done: primes = 1270607
```

串起来看：

- **0.331ms → 15.920ms，约 15.6ms 的空白**：G3 正在算，期间一个字符都没打。
  时间片是 `forcePreemptNS = 10ms`，实测被延长到 ~15ms —— 因为抢占是**异步**的：
  sysmon 轮询 + 发 `SIGURG` + 目标 G 要跑到一个安全点才能被切走，开销叠加在上面。
  （多次运行实测这个空白落在 10.9ms ~ 15.7ms 之间浮动。）
- **15.920ms G4 开嗓**：G3 被抢占后进了**全局队列**，而 G4 还躺在**本地环形队列**里。
  `findRunnable` 是**先查本地队列、再查全局队列**的，所以 **G4 抢在 G3 前面跑**。
- **136.299ms G3 才报 `done:`**：G3 把 CPU 拿回来接着算完剩下的活儿才收工。

> **铁证**：G3 的**创建顺序在 G4 之前**（`main: created G3` 在 `main: created G4` 前面），
> G3 的**启动也早于 G4**（0.331ms < 15.920ms），
> 可 G3 的 **`done:` 行却排在 G4 全部 40 行之后**。
> **只可能是 G3 中途被抢走过。**
>
> 注意这**不影响**阶段 4 的验证 —— 阶段 4 比的是「**第一次打印**顺序」，
> G3 的第一次打印是 0.331ms 的 START 行，仍在第 4 位。

### 5.4 `-heavy=false` 模式：零噪音的纯队列顺序

同样跑一遍，关键行：

```
[G5 +    0.286ms]  G5 START — 中文 + emoji
[G1 +    0.308ms]  G1 START — 打印大写字母表
[G2 +    0.316ms]  G2 START — 打印数字 01..50
[G3 +    0.332ms]  G3 START — 轻量模式 (-heavy=false), 只打这一行就结束
[G4 +    0.336ms]  G4 START — 打印 ASCII 符号图案 #=+*
```

**五个 G 从 0.286ms 到 0.336ms，50 微秒内全部跑完，中间一个字符都没有交错**，
顺序干干净净就是 **G5 → G1 → G2 → G3 → G4**。

这就是「把噪音拿掉之后」的样子 —— **多 P 时你根本看不到这个顺序**，
因为大家被分头扔到 8 个 P 上并行执行去了。

### 5.5 阶段 4 的自检

```
--- 阶段 4: 验证 ---
预测的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
实际的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
结论: ✅ 一致 —— 单 P 下 runnext(LIFO) + ring(FIFO) 的预测成立
```

**实测连跑 3 次，「实际的第一个打印顺序」每次都是 `G5 → G1 → G2 → G3 → G4`，与预测一致。**

（如果哪次不一致，程序会打印 ❌ 并提示：顺序是实现细节，换 Go 版本 / 换机器 / 加 `-race` 都可能变。）

### 5.6 对照：同一个程序，`-race` 构建下顺序彻底乱了

**这是全篇最有说服力的一条。** 同一个源码、同一个目录，只换一个编译标志：

```bash
$ for i in 1 2 3 4; do go run listing01_five_p1.go | grep "实际的第一个打印顺序"; done
实际的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
实际的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
实际的第一个打印顺序 : G5 → G1 → G2 → G3 → G4
实际的第一个打印顺序 : G5 → G1 → G2 → G3 → G4

$ for i in 1 2 3 4; do go run -race listing01_five_p1.go | grep "实际的第一个打印顺序"; done
实际的第一个打印顺序 : G4 → G1 → G2 → G3 → G5
实际的第一个打印顺序 : G3 → G1 → G2 → G4 → G5
实际的第一个打印顺序 : G5 → G2 → G3 → G1 → G4
实际的第一个打印顺序 : G3 → G1 → G2 → G4 → G5
```

**普通构建 4/4 次一模一样；`-race` 构建 4/4 次各不相同。**

原因在 `runtime/proc.go`：

```go
const randomizeScheduler = raceenabled

func runqput(pp *p, gp *g, next bool) {
	...
	if randomizeScheduler && next && randn(2) == 0 {
		next = false        // ← 50% 概率放弃 runnext 槽, 直接进环形队列
	}
}
```

竞态检测器构建下 `raceenabled == true`，`runqput` 有 **50% 的概率不走 runnext** ——
**VIP 槽待遇没了，顺序也就随机了。**

> 这一条直接推翻了「顺序是固定的」这种直觉。
> 记住：**单 P + 普通构建下顺序稳定，是因为它恰好是当前实现的默认路径；
> 换成 `-race` 构建，同一份代码的顺序就完全不可预测。**
> 所以业务代码里**永远不要依赖 goroutine 的执行顺序**。

---

## 6. 源码核对表

每条断言都对着 `$GOROOT/src/runtime/`（go1.27.4 → `/snap/go/11411/src/runtime/`）实际核过：

| 断言 | 出处 |
|---|---|
| 新 G 进 `runnext` VIP 槽 | `proc.go` `runqput()`：`if next { ... pp.runnext.cas(oldnext, ...) }` |
| 旧 runnext 被踢进**队列尾部**（LIFO 的来源） | `proc.go` `runqput()`：`// Kick the old runnext out to the regular run queue.` |
| 本地队列是 **256 格**环形数组 | `runtime2.go`：`runq [256]guintptr` |
| 取任务**先 runnext 后 ring 队头**（FIFO） | `proc.go` `runqget()`：先 CAS `pp.runnext`；再 `gp := pp.runq[h%uint32(len(pp.runq))].ptr()`，`h` 递增 |
| 时间片 **10ms** | `proc.go`：`const forcePreemptNS = 10 * 1000 * 1000 // 10ms` |
| 被抢占的 G 进**全局**队列 | `proc.go` `gopreempt_m()` → `goschedImpl(gp, true)` → `globrunqput(gp)` |
| **本地队列优先于全局队列**（所以 G4 抢先于 G3） | `proc.go` `findRunnable()`：`// local runq` 的 `runqget(pp)` 在 `// global runq` 的 `globrunqgetbatch()` **之前** |
| 竞态构建下调度器随机化 | `proc.go`：`const randomizeScheduler = raceenabled`；`runqput()` 里 `if randomizeScheduler && next && randn(2) == 0 { next = false }` |
| `GOMAXPROCS(0)` 只读不改 | `proc.go` `GOMAXPROCS()`：`if n < 1 { return int(gomaxprocs) }` |

---

## 7. 附录：schedtrace —— 让 runtime 自己把队列状态打印出来

### 7.1 为什么不能直接用任务里给的命令

```bash
GODEBUG=schedtrace=1000 go run listing01_five_p1.go 2>&1 | head -60
```

实测输出（**全部 5 行都是 `gomaxprocs=8`**，而且都在 0ms）：

```
SCHED 0ms: gomaxprocs=8 idleprocs=5 threads=6 spinningthreads=1 needspinning=0 idlethreads=0 runqueue=0 [1 0 0 0 0 0 0 0]
SCHED 0ms: gomaxprocs=8 idleprocs=5 threads=6 spinningthreads=1 needspinning=0 idlethreads=0 runqueue=0 [1 0 0 0 0 0 0 0]
SCHED 0ms: gomaxprocs=8 idleprocs=5 threads=5 spinningthreads=1 needspinning=0 idlethreads=0 runqueue=0 [1 0 0 0 0 0 0 0]
SCHED 0ms: gomaxprocs=8 idleprocs=6 threads=3 spinningthreads=1 needspinning=0 idlethreads=0 runqueue=0 [ 0 0 0 0 0 0 0 0 ] schedticks=[ 0 0 0 0 0 0 0 0 ]
SCHED 0ms: gomaxprocs=8 idleprocs=6 threads=4 spinningthreads=1 needspinning=0 idlethreads=0 runqueue=0 [ 1 0 0 0 0 0 0 0 ] schedticks=[ 0 0 0 0 0 0 0 0 ]
```

**两个坑：**

1. **`go run` 的编译/链接子进程也继承了 `GODEBUG`**，它们是独立的 Go 程序、各有自己的调度器，
   所以前面这堆 `gomaxprocs=8` 是**编译器**的 trace，不是你的程序的。
2. **`schedtrace=1000` 是每 1000ms 打一次**，而本程序只跑 ~140ms ——
   第一次真正的周期 trace 还没到，程序就退出了。

### 7.2 能用的做法：跑编译好的二进制 + 细一点的间隔

```bash
go build -o /tmp/p1bin listing01_five_p1.go
GODEBUG=schedtrace=5 /tmp/p1bin 2>&1 >/dev/null
```

实测输出（**这才是本程序自己的 trace**）：

```
SCHED 0ms: gomaxprocs=8 idleprocs=5 threads=5 spinningthreads=1 needspinning=0 idlethreads=0 runqueue=0 [ 0 0 0 0 0 0 0 0 ] schedticks=[ 0 0 0 0 0 0 0 2 ]
SCHED 4ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 2 ] schedticks=[ 5 ]
SCHED 10ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 2 ] schedticks=[ 5 ]
SCHED 15ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 2 ] schedticks=[ 5 ]
SCHED 25ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 1 ] schedticks=[ 9 ]
SCHED 35ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 1 ] schedticks=[ 9 ]
SCHED 46ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 1 ] schedticks=[ 11 ]
SCHED 56ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 1 ] schedticks=[ 11 ]
SCHED 66ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=1 [ 0 ] schedticks=[ 13 ]
SCHED 76ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=1 [ 0 ] schedticks=[ 13 ]
SCHED 86ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 0 ] schedticks=[ 15 ]
SCHED 96ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 0 ] schedticks=[ 15 ]
SCHED 106ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 0 ] schedticks=[ 16 ]
SCHED 116ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 0 ] schedticks=[ 16 ]
SCHED 127ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 0 ] schedticks=[ 17 ]
SCHED 137ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 0 ] schedticks=[ 17 ]
SCHED 147ms: gomaxprocs=1 idleprocs=0 threads=5 spinningthreads=0 needspinning=1 idlethreads=3 runqueue=0 [ 0 ] schedticks=[ 18 ]
```

### 7.3 一句话解读：每个字段是什么，能看出什么

| 字段 | 含义 | 这份 trace 说明了什么 |
|---|---|---|
| `gomaxprocs=1` | 逻辑处理器数 | **第 2 行起变成 1** → `runtime.GOMAXPROCS(1)` 生效了（第 1 行还是 8，那是 `main` 还没执行到） |
| `[ N ]` | **每个 P 的本地环形队列长度**（`runqtail - runqhead`） | 第 2 行起**只有 1 个元素** → 只剩 P0 一个 P；`[ 2 ] → [ 1 ] → [ 0 ]` 就是本地环被逐步取空 |
| `runqueue=N` | **全局队列**长度 | 66/76ms 时 `runqueue=1` 而本地环是 `[ 0 ]` → 有 goroutine 躺在全局队列里 |
| `schedticks=[ N ]` | 每个 P 的调度器 tick 计数 | 从 5 爬到 18 ≈ **每 ~10ms +1** → **10ms 时间片的直接证据** |

**本地环里那几个 G 到底是谁？** 用 `GOGC=off` 排除掉 GC 就能看清：

```bash
GOGC=off GODEBUG=schedtrace=10 /tmp/p1bin 2>&1 >/dev/null | grep gomaxprocs=1
```

```
SCHED 15ms: gomaxprocs=1 ... runqueue=0 [ 1 ] schedticks=[ 5 ]
SCHED 25ms: gomaxprocs=1 ... runqueue=0 [ 0 ] schedticks=[ 7 ]
SCHED 35ms: gomaxprocs=1 ... runqueue=0 [ 0 ] schedticks=[ 7 ]
...
```

**`[ 1 ]` 恰好就是 G4**（本地环里剩的唯一一个演示 goroutine），下一拍变成 `[ 0 ]` —— 它被取走了。
开着 GC 时那个多出来的 `[ 2 ]` 里的第二个，是 **GC mark worker**：
G3 分配 20MB 触发了 GC，runtime 往 P0 的本地队列里塞了一个 GC 工作协程。
**它不是演示用的 goroutine**，用 `GOGC=off` 对照即可排除。

---

## 8. 三条观察建议

### 建议 1：先用 `-heavy=false` 跑一遍，再跑默认的 heavy

`-heavy=false` 那次是**零噪音**的：五个 G 在 50 微秒内跑完，顺序干干净净
`G5 → G1 → G2 → G3 → G4`，每一块的输出都不交错。
**先把"没有抢占干扰时队列长什么样"看清楚**，再看 heavy 模式里 G3/G4 的纠缠，
就知道多出来的那 15ms 空白意味着什么。

### 建议 2：盯住 G3 的 START 行和 G4 的 START 行之间那段空白

```
[G3 +    0.331ms]  G3 START — CPU 密集筛素数 ...
        ←←←←←←←← 空白约 15.6ms ←←←←←←←←
[G4 +   15.920ms]  G4 START — 打印 ASCII 符号图案 #=+*
```

**这段空白就是「G3 被抢占、G4 插队」的窗口。**
想让窗口更明显，把 `sieveLimit` 调大（G3 跑得更久，被抢更多次）；
想看窗口收窄，调小到 `5000000`（≈19ms，刚够一个多时间片）。
`GOGC=off` 可以顺便把 GC worker 这个干扰项去掉。

### 建议 3：把阶段 1 的预测块和阶段 4 的实际结果并排看

这是整个实验的闭环：**你手写的预测**（阶段 1，每次创建后打印）
和 **runtime 的实际行为**（阶段 4，程序自动采集并比对）。

预测对了，说明你真的读懂了 `runqput` / `runqget` 这两个函数；
预测错了，程序会直接告诉你 ❌ —— 那说明实现细节和你的理解有偏差，
**这时候再翻源码 `proc.go` 找原因，比从头看一遍有效得多。**

最后再强调一次：**这个顺序是实现细节，不是语言保证。**
`go run -race listing01_five_p1.go` 会因为 `randomizeScheduler = raceenabled`
而彻底打乱顺序（大约一半的概率不走 runnext 槽）。
写业务代码时，唯一能依赖的只有 `WaitGroup`（"都跑完了"）和 `channel`（"数据同步了"）。

---

*本文所有输出均为本机（go1.27.4 linux/amd64，8 逻辑核）实跑所得，未经编辑。*
*`listing01.go` / `listing01_five.go` / `listing04.go` / `listing07.go` / `doc.go` / `go.mod` / `README.md` / `docs/listing01_five_notes.md` 均未改动。*
