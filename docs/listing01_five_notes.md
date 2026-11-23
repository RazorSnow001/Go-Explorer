# listing01_five.go 实验说明

> 衍生自 `listing01.go`（Listing 6.1）的调度器观察实验。
> 目标：把 `runnext` VIP 槽的 **LIFO**、本地环形队列的 **FIFO**、以及超过时间片后的**抢占交错**三件事，一次性变成肉眼可见的输出。

---

## 1. 运行环境

| 项 | 值 |
|---|---|
| Go | `go version go1.27.4 linux/amd64` |
| 平台 | `linux/amd64`（WSL2） |
| CPU 逻辑核 | 8（`runtime.NumCPU() == 8`） |
| 目录 | `/home/changqi/Go-Explorer/` |
| 第三方依赖 | 无 |

跑法：

```bash
cd /home/changqi/Go-Explorer

go run listing01_five.go                      # 默认，8 个 P
go run -race listing01_five.go                # 竞态检测器构建
GOMAXPROCS=1 go run listing01_five.go         # 单 P，最干净
```

> 文件带 `//go:build ignore`，所以 `go build ./...` / `go vet ./...` 会跳过它，
> 但**显式写在命令行上的文件不受构建约束限制**，三个 `go run` 都正常工作。
> 和 `listing01.go` / `listing04.go` / `listing07.go` 是同一套规矩。

---

## 2. 代码结构说明

### 2.1 整体骨架

和 `listing01.go` 完全同构：`package main` → `func main()` → `sync.WaitGroup` 记账 → 起 goroutine → `wg.Wait()` 收尾。

差别只有三处，都是为了"看得见"：

1. **每行输出都带前缀** `[Gx +12.345ms]` —— 前缀是 goroutine 身份，时间戳是相对程序启动的毫秒数。
   没有时间戳就看不出交错；有了时间戳，一眼能看出"谁在什么时候抢到了 CPU"。
2. **main 在每个 `go` 语句后打印一行** `main: created Gx` —— 把**创建顺序**显式摊开，
   好和**输出顺序**对照。
3. **程序自己不调用 `runtime.GOMAXPROCS(n)`**，只读当前值（`runtime.GOMAXPROCS(0)`）并打印。
   这样 `GOMAXPROCS=1 go run ...` 这个环境变量才真正生效，能和默认的 8 P 做对照。

### 2.2 五个 goroutine 各自的定位

| G | 捕获的东西 | 打印内容 | 工作量 | 观察点 |
|---|---|---|---|---|
| **G1** | 外层 `alphabet` 字符串 | 大写字母表 A–Z，连打 3 轮（自带 `round` 循环变量） | 极轻（3 次 Printf，约 0.01ms） | **runnext 整块输出**，绝不交错 |
| **G2** | 外层 `numberCount`（= 50） | `01`–`50`，每 10 个一行 | 中等（50 次 Printf，约 0.05ms） | 环形队列里排第 2 |
| **G3** | 外层 `sieveLimit`（= 20000000） | 计算期间**不打任何东西**，只有开头一行 `start:` 和结尾一行 `done: primes = 1270607` | **极重**：CPU 密集筛素数，本机实测 **≈140ms**（是 10ms 时间片的 14 倍） | **被抢占的现场** |
| **G4** | 外层 `pattern` 字符串 + `patternRows`（= 40） | `#=+*` 符号图案，40 行 | 中等（40 次 Printf，约 0.1ms） | 从被抢占的 G3 手里**抢走 CPU** 的那个 |
| **G5** | **字面量**（不捕获变量） | 中文句子 + emoji，10 行 | 中等（10 次 Printf，约 0.03ms） | **runnext VIP 槽的常客** |

**捕获方式各不相同**：G1 捕获字符串、G2 捕获整数、G3 捕获计算上界、G4 同时捕获两个、G5 什么也不捕获、全用字面量。

### 2.3 G3 为什么选"筛素数"

需要的是一个**纯 CPU、结果确定、单次远超 10ms** 的活儿。筛素数刚好三条都占：

- 纯计算，不碰 I/O，不会因为写 stdout 而意外让出 CPU；
- 结果确定（1270607 个素数），每次跑可以对得上；
- 时间可控。本机实测：

  | `sieveLimit` | 素数个数 | 耗时 |
  |---|---|---|
  | 2,000,000 | 148,933 | 6.7ms |
  | 5,000,000 | 348,513 | 19.4ms |
  | 10,000,000 | 664,579 | 46.5ms |
  | **20,000,000** | **1,270,607** | **137.1ms** ✅ 选它 |
  | 40,000,000 | 2,433,654 | 339.2ms |

  选 20M：137ms ≈ 14 个时间片，足够被抢占十几次；又不会让整个实验跑得太久。

---

## 3. 预期观察点（写死在代码注释里，也打印在 stdout 上）

程序开头会先把这套假设打出来，方便你事后核对：

```
main: hypothesis on a single P:
main:     runnext VIP slot (LIFO) holds the LAST one created = G5
main:     local ring buffer (FIFO) holds G1 -> G2 -> G3 -> G4
main:     so expect: G5 -> G1 -> G2 -> G3 -> G4
```

机制拆开说（每一条都在 Go 源码里核对过，见第 5 节）：

**① `runnext` VIP 槽是 LIFO**

每执行一次 `go func(){}`，runtime 走 `newproc` → `runqput(pp, g, next=true)`。
`next=true` 表示新 goroutine 进 `pp.runnext` 这个单槽位（VIP 槽）；
**如果槽里已经有人，被挤下来的那个被塞进本地环形队列的尾部**。

所以依次创建 G1→G5 之后：

```
runnext (VIP, LIFO) : G5
本地环形队列 (FIFO)   : G1 -> G2 -> G3 -> G4
```

main 阻塞在 `wg.Wait()` 时，调度器**先取 runnext**，所以**最后创建的 G5 第一个跑**。

**② 本地环形队列是 FIFO**

`runnext` 里的 G5 跑完后，调度器从环形队列**头部**取 —— 头部是 G1（G1 最早创建，
当初 G2 挤进 runnext 时它被踢到队尾，而那时队列是空的，它就成了队头）。
依次取出 G1 → G2 → G3 → G4，**严格按创建顺序**。

**③ 超过时间片会被抢占**

G3 要跑 ≈140ms，远超调度器的时间片
（`runtime/proc.go`: `const forcePreemptNS = 10 * 1000 * 1000 // 10ms`）。
到期后 sysmon 发信号异步抢占，G3 被推回**全局队列**。
而本地队列里还剩 G4 —— `findRunnable` 是**先查本地队列、再查全局队列**的，
所以 G4 抢在 G3 前面跑。

结果就是：**G3 的创建顺序在 G4 之前、启动也早于 G4，但 G3 的 `done:` 行出现在 G4 全部 40 行之后。**
这就是抢占交错的铁证。

---

## 4. 三次运行的完整输出（原样贴出，未删减）

### 运行 1 —— 普通构建（默认 8 个 P）

命令：`go run listing01_five.go`

```
main: GOMAXPROCS = 8 (logical processors available)
main: NumCPU     = 8

main: creating goroutines in the order G1 -> G2 -> G3 -> G4 -> G5
main: hypothesis on a single P:
main:     runnext VIP slot (LIFO) holds the LAST one created = G5
main:     local ring buffer (FIFO) holds G1 -> G2 -> G3 -> G4
main:     so expect: G5 -> G1 -> G2 -> G3 -> G4

main: created G1 (light, uppercase alphabet, 3 rounds)
main: created G2 (medium, 50 numbers, 10 per line)
main: created G3 (heavy, CPU bound sieve, ~140ms >> 10ms time slice)
main: created G4 (symbols, 40 rows of #=+*)
main: created G5 (chinese + emoji, 10 lines) -- LAST created, expect it FIRST

main: all five goroutines created, calling wg.Wait()
[G5 +    0.099ms] 第 1 行：你好，Go 并发 🐹
[G5 +    0.125ms] 第 2 行：你好，Go 并发 🐹
[G5 +    0.128ms] 第 3 行：你好，Go 并发 🐹
[G5 +    0.130ms] 第 4 行：你好，Go 并发 🐹
[G5 +    0.131ms] 第 5 行：你好，Go 并发 🐹
[G5 +    0.133ms] 第 6 行：你好，Go 并发 🐹
[G5 +    0.135ms] 第 7 行：你好，Go 并发 🐹
[G5 +    0.136ms] 第 8 行：你好，Go 并发 🐹
[G5 +    0.137ms] 第 9 行：你好，Go 并发 🐹
[G5 +    0.139ms] 第 10 行：你好，Go 并发 🐹
[G3 +    0.143ms]  start: sieving primes up to 20000000
[G4 +    0.182ms] 1  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.207ms] 2  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.209ms] 3  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.210ms] 4  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.211ms] 5  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.212ms] 6  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.213ms] 7  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.214ms] 8  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.216ms] 9  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.219ms] 10 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.220ms] 11 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.221ms] 12 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.222ms] 13 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.223ms] 14 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.224ms] 15 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.225ms] 16 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.226ms] 17 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.227ms] 18 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.227ms] 19 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.228ms] 20 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.229ms] 21 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.230ms] 22 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.231ms] 23 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.232ms] 24 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.234ms] 25 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.236ms] 26 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.237ms] 27 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.238ms] 28 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.239ms] 29 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.242ms] 30 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.243ms] 31 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.244ms] 32 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.245ms] 33 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.246ms] 34 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.247ms] 35 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.248ms] 36 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.249ms] 37 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.250ms] 38 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.251ms] 39 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.252ms] 40 #=+*#=+*#=+*#=+*#=+*
[G1 +    0.229ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )
[G1 +    0.616ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 2 )
[G1 +    0.618ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 3 )
[G2 +    0.151ms] 01 [G2 +    0.622ms] 02 [G2 +    0.623ms] 03 [G2 +    0.624ms] 04 [G2 +    0.625ms] 05 [G2 +    0.626ms] 06 [G2 +    0.627ms] 07 [G2 +    0.628ms] 08 [G2 +    0.630ms] 09 [G2 +    0.631ms] 10 
[G2 +    0.632ms] 11 [G2 +    0.634ms] 12 [G2 +    0.635ms] 13 [G2 +    0.639ms] 14 [G2 +    0.640ms] 15 [G2 +    0.641ms] 16 [G2 +    0.643ms] 17 [G2 +    0.644ms] 18 [G2 +    0.645ms] 19 [G2 +    0.646ms] 20 
[G2 +    0.647ms] 21 [G2 +    0.649ms] 22 [G2 +    0.650ms] 23 [G2 +    0.651ms] 24 [G2 +    0.652ms] 25 [G2 +    0.653ms] 26 [G2 +    0.654ms] 27 [G2 +    0.655ms] 28 [G2 +    0.656ms] 29 [G2 +    0.661ms] 30 
[G2 +    0.662ms] 31 [G2 +    0.663ms] 32 [G2 +    0.664ms] 33 [G2 +    0.665ms] 34 [G2 +    0.666ms] 35 [G2 +    0.668ms] 36 [G2 +    0.669ms] 37 [G2 +    0.670ms] 38 [G2 +    0.671ms] 39 [G2 +    0.672ms] 40 
[G2 +    0.674ms] 41 [G2 +    0.675ms] 42 [G2 +    0.676ms] 43 [G2 +    0.677ms] 44 [G2 +    0.681ms] 45 [G2 +    0.688ms] 46 [G2 +    0.690ms] 47 [G2 +    0.691ms] 48 [G2 +    0.692ms] 49 [G2 +    0.693ms] 50 
[G3 +  142.673ms]  done: primes = 1270607

main: all goroutines finished
TerminatingProgram
```

### 运行 2 —— 竞态检测器构建（`randomizeScheduler` 生效，顺序被打乱）

命令：`go run -race listing01_five.go`

```
main: GOMAXPROCS = 8 (logical processors available)
main: NumCPU     = 8

main: creating goroutines in the order G1 -> G2 -> G3 -> G4 -> G5
main: hypothesis on a single P:
main:     runnext VIP slot (LIFO) holds the LAST one created = G5
main:     local ring buffer (FIFO) holds G1 -> G2 -> G3 -> G4
main:     so expect: G5 -> G1 -> G2 -> G3 -> G4

main: created G1 (light, uppercase alphabet, 3 rounds)
main: created G2 (medium, 50 numbers, 10 per line)
main: created G3 (heavy, CPU bound sieve, ~140ms >> 10ms time slice)
main: created G4 (symbols, 40 rows of #=+*)
main: created G5 (chinese + emoji, 10 lines) -- LAST created, expect it FIRST

main: all five goroutines created, calling wg.Wait()
[G4 +    0.276ms] 1  #=+*#=+*#=+*#=+*#=+*
[G2 +    0.231ms] 01 [G4 +    0.309ms] 2  #=+*#=+*#=+*#=+*#=+*
[G2 +    0.331ms] 02 [G4 +    0.347ms] 3  #=+*#=+*#=+*#=+*#=+*
[G2 +    0.352ms] 03 [G4 +    0.357ms] 4  #=+*#=+*#=+*#=+*#=+*
[G2 +    0.361ms] 04 [G4 +    0.369ms] 5  #=+*#=+*#=+*#=+*#=+*
[G2 +    0.370ms] 05 [G4 +    0.382ms] 6  #=+*#=+*#=+*#=+*#=+*
[G2 +    0.389ms] 06 [G1 +    0.304ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )
[G1 +    0.416ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 2 )
[G1 +    0.432ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 3 )
[G3 +    0.364ms]  start: sieving primes up to 20000000
[G4 +    0.396ms] 7  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.478ms] 8  #=+*#=+*#=+*#=+*#=+*
[G4 +    0.493ms] 9  #=+*#=+*#=+*#=+*#=+*
[G2 +    0.404ms] 07 [G2 +    0.521ms] 08 [G2 +    0.534ms] 09 [G2 +    0.577ms] 10 
[G2 +    0.591ms] 11 [G2 +    0.607ms] 12 [G4 +    0.505ms] 10 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.639ms] 11 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.690ms] 12 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.704ms] 13 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.712ms] 14 #=+*#=+*#=+*#=+*#=+*
[G4 +    0.721ms] 15 #=+*#=+*#=+*#=+*#=+*
[G2 +    0.615ms] 13 [G5 +    0.609ms] 第 1 行：你好，Go 并发 🐹
[G2 +    1.075ms] 14 [G2 +    1.453ms] 15 [G2 +    1.474ms] 16 [G4 +    1.074ms] 16 #=+*#=+*#=+*#=+*#=+*
[G2 +    1.495ms] 17 [G2 +    1.510ms] 18 [G2 +    1.526ms] 19 [G2 +    1.532ms] 20 
[G4 +    1.507ms] 17 #=+*#=+*#=+*#=+*#=+*
[G2 +    1.550ms] 21 [G2 +    1.559ms] 22 [G5 +    1.315ms] 第 2 行：你好，Go 并发 🐹
[G4 +    1.555ms] 18 #=+*#=+*#=+*#=+*#=+*
[G5 +    1.605ms] 第 3 行：你好，Go 并发 🐹
[G4 +    1.616ms] 19 #=+*#=+*#=+*#=+*#=+*
[G4 +    1.630ms] 20 #=+*#=+*#=+*#=+*#=+*
[G5 +    1.625ms] 第 4 行：你好，Go 并发 🐹
[G4 +    1.645ms] 21 #=+*#=+*#=+*#=+*#=+*
[G4 +    1.656ms] 22 #=+*#=+*#=+*#=+*#=+*
[G5 +    1.654ms] 第 5 行：你好，Go 并发 🐹
[G4 +    1.670ms] 23 #=+*#=+*#=+*#=+*#=+*
[G5 +    1.677ms] 第 6 行：你好，Go 并发 🐹
[G5 +    1.702ms] 第 7 行：你好，Go 并发 🐹
[G5 +    1.721ms] 第 8 行：你好，Go 并发 🐹
[G5 +    1.738ms] 第 9 行：你好，Go 并发 🐹
[G2 +    1.570ms] 23 [G4 +    1.684ms] 24 #=+*#=+*#=+*#=+*#=+*
[G2 +    1.763ms] 24 [G2 +    1.790ms] 25 [G4 +    1.766ms] 25 #=+*#=+*#=+*#=+*#=+*
[G4 +    1.833ms] 26 #=+*#=+*#=+*#=+*#=+*
[G4 +    1.859ms] 27 #=+*#=+*#=+*#=+*#=+*
[G5 +    1.748ms] 第 10 行：你好，Go 并发 🐹
[G4 +    1.893ms] 28 #=+*#=+*#=+*#=+*#=+*
[G4 +    1.920ms] 29 #=+*#=+*#=+*#=+*#=+*
[G2 +    1.810ms] 26 [G4 +    1.942ms] 30 #=+*#=+*#=+*#=+*#=+*
[G4 +    2.019ms] 31 #=+*#=+*#=+*#=+*#=+*
[G2 +    1.991ms] 27 [G4 +    2.033ms] 32 #=+*#=+*#=+*#=+*#=+*
[G4 +    2.075ms] 33 #=+*#=+*#=+*#=+*#=+*
[G2 +    2.062ms] 28 [G4 +    2.125ms] 34 #=+*#=+*#=+*#=+*#=+*
[G2 +    2.100ms] 29 [G2 +    2.168ms] 30 
[G2 +    2.188ms] 31 [G2 +    2.212ms] 32 [G4 +    2.156ms] 35 #=+*#=+*#=+*#=+*#=+*
[G2 +    2.220ms] 33 [G2 +    2.238ms] 34 [G2 +    2.247ms] 35 [G4 +    2.234ms] 36 #=+*#=+*#=+*#=+*#=+*
[G2 +    2.264ms] 36 [G4 +    2.311ms] 37 #=+*#=+*#=+*#=+*#=+*
[G2 +    2.374ms] 37 [G4 +    2.368ms] 38 #=+*#=+*#=+*#=+*#=+*
[G4 +    2.412ms] 39 #=+*#=+*#=+*#=+*#=+*
[G2 +    2.426ms] 38 [G2 +    2.443ms] 39 [G2 +    2.459ms] 40 
[G2 +    2.476ms] 41 [G2 +    2.487ms] 42 [G2 +    2.508ms] 43 [G4 +    2.434ms] 40 #=+*#=+*#=+*#=+*#=+*
[G2 +    2.532ms] 44 [G2 +    2.550ms] 45 [G2 +    2.571ms] 46 [G2 +    2.587ms] 47 [G2 +    2.602ms] 48 [G2 +    2.610ms] 49 [G2 +    2.631ms] 50 
[G3 + 1131.424ms]  done: primes = 1270607

main: all goroutines finished
TerminatingProgram
```

### 运行 3 —— 单 P（`GOMAXPROCS=1`，三个机制全部干净可见）

命令：`GOMAXPROCS=1 go run listing01_five.go`

```
main: GOMAXPROCS = 1 (logical processors available)
main: NumCPU     = 8

main: creating goroutines in the order G1 -> G2 -> G3 -> G4 -> G5
main: hypothesis on a single P:
main:     runnext VIP slot (LIFO) holds the LAST one created = G5
main:     local ring buffer (FIFO) holds G1 -> G2 -> G3 -> G4
main:     so expect: G5 -> G1 -> G2 -> G3 -> G4

main: created G1 (light, uppercase alphabet, 3 rounds)
main: created G2 (medium, 50 numbers, 10 per line)
main: created G3 (heavy, CPU bound sieve, ~140ms >> 10ms time slice)
main: created G4 (symbols, 40 rows of #=+*)
main: created G5 (chinese + emoji, 10 lines) -- LAST created, expect it FIRST

main: all five goroutines created, calling wg.Wait()
[G5 +    0.063ms] 第 1 行：你好，Go 并发 🐹
[G5 +    0.076ms] 第 2 行：你好，Go 并发 🐹
[G5 +    0.078ms] 第 3 行：你好，Go 并发 🐹
[G5 +    0.079ms] 第 4 行：你好，Go 并发 🐹
[G5 +    0.079ms] 第 5 行：你好，Go 并发 🐹
[G5 +    0.080ms] 第 6 行：你好，Go 并发 🐹
[G5 +    0.081ms] 第 7 行：你好，Go 并发 🐹
[G5 +    0.082ms] 第 8 行：你好，Go 并发 🐹
[G5 +    0.083ms] 第 9 行：你好，Go 并发 🐹
[G5 +    0.084ms] 第 10 行：你好，Go 并发 🐹
[G1 +    0.086ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )
[G1 +    0.090ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 2 )
[G1 +    0.091ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 3 )
[G2 +    0.093ms] 01 [G2 +    0.095ms] 02 [G2 +    0.096ms] 03 [G2 +    0.097ms] 04 [G2 +    0.098ms] 05 [G2 +    0.099ms] 06 [G2 +    0.100ms] 07 [G2 +    0.101ms] 08 [G2 +    0.102ms] 09 [G2 +    0.103ms] 10 
[G2 +    0.104ms] 11 [G2 +    0.105ms] 12 [G2 +    0.106ms] 13 [G2 +    0.107ms] 14 [G2 +    0.107ms] 15 [G2 +    0.108ms] 16 [G2 +    0.109ms] 17 [G2 +    0.110ms] 18 [G2 +    0.111ms] 19 [G2 +    0.112ms] 20 
[G2 +    0.113ms] 21 [G2 +    0.114ms] 22 [G2 +    0.115ms] 23 [G2 +    0.116ms] 24 [G2 +    0.117ms] 25 [G2 +    0.118ms] 26 [G2 +    0.118ms] 27 [G2 +    0.119ms] 28 [G2 +    0.120ms] 29 [G2 +    0.121ms] 30 
[G2 +    0.122ms] 31 [G2 +    0.123ms] 32 [G2 +    0.124ms] 33 [G2 +    0.125ms] 34 [G2 +    0.126ms] 35 [G2 +    0.127ms] 36 [G2 +    0.127ms] 37 [G2 +    0.128ms] 38 [G2 +    0.129ms] 39 [G2 +    0.130ms] 40 
[G2 +    0.131ms] 41 [G2 +    0.132ms] 42 [G2 +    0.133ms] 43 [G2 +    0.134ms] 44 [G2 +    0.135ms] 45 [G2 +    0.136ms] 46 [G2 +    0.137ms] 47 [G2 +    0.137ms] 48 [G2 +    0.138ms] 49 [G2 +    0.139ms] 50 
[G3 +    0.141ms]  start: sieving primes up to 20000000
[G4 +   15.367ms] 1  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.402ms] 2  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.403ms] 3  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.405ms] 4  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.406ms] 5  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.406ms] 6  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.407ms] 7  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.408ms] 8  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.409ms] 9  #=+*#=+*#=+*#=+*#=+*
[G4 +   15.410ms] 10 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.411ms] 11 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.412ms] 12 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.413ms] 13 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.414ms] 14 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.415ms] 15 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.416ms] 16 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.417ms] 17 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.418ms] 18 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.419ms] 19 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.420ms] 20 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.421ms] 21 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.422ms] 22 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.422ms] 23 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.423ms] 24 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.424ms] 25 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.425ms] 26 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.426ms] 27 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.427ms] 28 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.428ms] 29 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.429ms] 30 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.430ms] 31 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.431ms] 32 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.432ms] 33 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.433ms] 34 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.434ms] 35 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.435ms] 36 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.436ms] 37 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.437ms] 38 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.448ms] 39 #=+*#=+*#=+*#=+*#=+*
[G4 +   15.449ms] 40 #=+*#=+*#=+*#=+*#=+*
[G3 +  154.336ms]  done: primes = 1270607

main: all goroutines finished
TerminatingProgram
```
---

## 5. 「怎么读这份输出」

三次运行分别暴露了三个机制，下面逐段拆。

### 5.1 先看运行 3（单 P）—— 三个机制一次看全

这是**最干净**的一份，先用它把模型建立起来。

**① runnext VIP 槽的 LIFO 证据（最前面几行）**

```
main: created G1 (light, uppercase alphabet, 3 rounds)
main: created G2 (medium, 50 numbers, 10 per line)
main: created G3 (heavy, CPU bound sieve, ~140ms >> 10ms time slice)
main: created G4 (symbols, 40 rows of #=+*)
main: created G5 (chinese + emoji, 10 lines) -- LAST created, expect it FIRST
...
[G5 +    0.063ms] 第 1 行：你好，Go 并发 🐹     ← 第一个出声的，是最后创建的 G5
```

上面 5 行 `main: created ...` 是**创建顺序**（G1→G5），紧跟着的第一条 goroutine 输出却是 **G5**。
**创建顺序和执行顺序正好相反** —— 这就是 LIFO。

> 对照 `listing01.go`：那里也是第二个 `go func()`（大写）先跑，同一个道理。

**② 环形队列 FIFO 的证据（接着往下）**

```
[G5 +    0.063ms] ... 第 10 行  ...     ← G5 跑完（runnext 用掉了）
[G1 +    0.086ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )   ← G1 第二个跑
[G1 +    0.090ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 2 )
[G1 +    0.091ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 3 )
[G2 +    0.093ms] 01 ...                        ← G2 第三个跑
[G2 +    0.139ms] ... 50 ...
[G3 +    0.141ms]  start: sieving primes up to 20000000   ← G3 第四个跑
```

**G5 → G1 → G2 → G3 → G4**，和代码里打印的假设**逐条对上**。
关键是 G1：它创建时先进了 runnext，被 G2 挤下来落到环形队列**队头**，所以它第二个跑。
**被挤下 VIP 槽的那个反而最先从队列里出来** —— 因为队列是 FIFO，队头就是最早创建的。

**③ 抢占交错的证据（时间戳出现大跳跃）**

```
[G3 +    0.141ms]  start: sieving primes up to 20000000
                        ↓ 这里空了一整段，什么都没打印
[G4 +   15.367ms] 1  #=+*#=+*#=+*#=+*#=+*     ← 15.4ms 时，G4 突然开嗓
[G4 +   15.449ms] 40 #=+*#=+*#=+*#=+*#=+*     ← G4 一口气打完 40 行
[G3 +  154.336ms]  done: primes = 1270607     ← G3 最后才报数
```

把三件事串起来看：

- **0.141ms → 15.367ms 有约 15ms 的空白**：G3 正在算，期间一个字符都没打。
  时间片是 `forcePreemptNS = 10ms`，实测被延长到 ~15ms —— 因为抢占是**异步**的：
  sysmon 轮询 + 发 `SIGURG` + 目标 goroutine 要跑到一个安全点才能被切走，
  这段开销叠加在 10ms 之上（WSL2 上还有调度抖动）。
- **15.367ms G4 开嗓**：G3 被抢占后进了**全局队列**，G4 还躺在**本地队列**里。
  `findRunnable` 先查本地后查全局，所以 **G4 抢在 G3 前面跑**。
- **154.336ms G3 才报 `done:`**：G3 从 G4 手里把 CPU 拿回来，接着算到 137ms 的活干完才收工。

> **铁证**：G3 的**创建顺序在 G4 之前**（`main: created G3` 在 `main: created G4` 前面），
> G3 的**启动也早于 G4**（0.141ms < 15.367ms），
> 可 G3 的**结果行却排在 G4 全部 40 行之后**。
> **只可能是 G3 中途被抢走过。**

### 5.2 再看运行 1（默认 8 个 P）—— 真并行，环形队列反而看不见了

```
[G5 +    0.099ms] 第 1 行：你好，Go 并发 🐹     ← G5 仍然第一个（runnext）
[G3 +    0.143ms]  start: sieving primes up to 20000000
[G4 +    0.182ms] 1  #=+*...
[G2 +    0.151ms] 01 ...
[G1 +    0.229ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )
```

**G5 还是第一个** —— runnext 是 **per-P** 的，main 所在那个 P 的 VIP 槽，别的 P 抢不走它。

但接下来的时间戳告诉你：**G3、G4、G2、G1 的首行分别落在 0.143 / 0.182 / 0.151 / 0.229ms**，
挤在 **0.09ms 的窗口里** —— 它们**同时在跑**，各自占着一个不同的 P。
所以 8 个 P 的时候，环形队列的 FIFO 顺序**根本观察不到**（大家被分头放到 8 个 P 上并行执行了）。

多 P 下的另一个证据，在 G2 身上：

```
[G2 +    0.151ms] 01 [G2 +    0.622ms] 02 ... 03 ...
```

G2 打完 `01` 之后，**隔了 0.47ms 才接着打 `02`** —— 中间被调度器切走了。
（`fmt.Printf` 自身是原子的，所以只可能是两次 Printf *之间* 发生了切换，不会撕裂成半个字符。）

### 5.3 最后看运行 2（`-race`）—— 顺序被随机化打乱

```
[G4 +    0.276ms] 1  #=+*...     ← 第一个出声的是 G4，不是 G5！
[G2 +    0.231ms] 01 ...
[G1 +    0.304ms]  ABCDEFGHIJKLMNOPQRSTUVWXYZ  (round 1 )
[G3 +    0.364ms]  start: sieving primes up to 20000000
[G5 +    0.609ms] 第 1 行：你好，Go 并发 🐹    ← G5 最后才跑！
```

**创建顺序是 G1→G5，第一个跑的是 G4，G5 反而垫底。** 原因在 Go 源码里：

```go
// runtime/proc.go
const randomizeScheduler = raceenabled

func runqput(pp *p, gp *g, next bool) {
	...
	if randomizeScheduler && next && randn(2) == 0 {
		next = false        // ← 50% 概率放弃 runnext，直接进环形队列
	}
	...
}
```

竞态检测器构建下 `raceenabled == true`，于是 `runqput` 有 **50% 的概率不走 runnext**，
新 goroutine 直接进本地环形队列 —— **VIP 槽待遇没了，顺序也就随机了**。

另外注意 G3 的耗时：

```
[G3 + 1131.424ms]  done: primes = 1270607
```

同样的筛素数，普通构建只要 **142.673ms**，`-race` 下要 **1131.424ms** —— **慢了约 8 倍**。
race detector 给每一次内存访问插桩，G3 那 20MB 的 `[]bool` 被访问了几千万次。
**这也解释了为什么 `-race` 构建经常"慢得不像话"** —— 不是你的代码变慢了，是检测器的开销。

> `-race` 全程 **0 条 `WARNING: DATA RACE`**，退出码 0。
> 五个 goroutine 之间没有共享内存，符合预期。

---

## 6. 源码核对（本文每一条机制断言的出处）

Go 1.27.4（`go env GOROOT` → `/snap/go/11411/src/runtime/`）：

| 断言 | 出处 |
|---|---|
| 新 goroutine 进 VIP 槽，被挤下的进队列尾部 | `proc.go` `runqput()`：`// Kick the old runnext out to the regular run queue.` |
| 队列是 256 格环形数组 | `runtime2.go`：`runq [256]guintptr` |
| 取任务先 VIP 后队头（FIFO） | `proc.go` `runqget()`：先 CAS `pp.runnext`，再 `pp.runq[h%256]` 按 `h` 递增取 |
| 时间片 10ms | `proc.go`：`const forcePreemptNS = 10 * 1000 * 1000 // 10ms` |
| 被抢占的 goroutine 进**全局**队列 | `proc.go` `gopreempt_m()` → `goschedImpl(gp, true)` → `globrunqput(gp)` |
| 本地队列优先于全局队列 | `proc.go` `findRunnable()`：`// local runq` 的 `runqget(pp)` 在 `// global runq` 的 `globrunqgetbatch()` **之前** |
| `-race` 下调度器随机化 | `proc.go`：`const randomizeScheduler = raceenabled` + `runqput()` 里的 `randn(2) == 0` |

---

## 7. 一句话结论

> **`go` 语句只管"入队"，不管"先跑"：新建的 goroutine 先进 P 的 `runnext` VIP 槽（LIFO，所以最后创建的最先跑），被挤下去的那些按创建顺序（FIFO）排在本地环形队列里；而一旦某个 goroutine 的活儿超过 10ms 时间片，它就会被抢占、被推到队尾，让位给排在它前面的 goroutine —— 顺序是实现细节，永远不要依赖。**

---

## 8. 给你的 3 条观察建议

### 建议 1：跑 `GOMAXPROCS=1 go run listing01_five.go`，盯着"第一行是谁"

这是理解 runnext 最快的路径。

- 第一行是 **G5**（最后创建的那个）→ 你亲眼看到了 **LIFO**；
- 第二行是 **G1**（最早创建的那个）→ 你亲眼看到了 **FIFO**；
- 两者合起来就是 `runnext` + 环形队列这套结构。

然后把 `GOMAXPROCS=1` 去掉再跑一次，第一行仍然是 G5，但接下来的顺序会乱成一团 —— 因为 8 个 P 会分头执行。

### 建议 2：盯着 G3 和 G4 之间的那个"时间黑洞"

单 P 那次运行里，`[G3 + 0.141ms] start:` 和 `[G3 + 154.336ms] done:` 中间夹着 G4 的 40 行。

**中间那 15ms 的空白，就是 G3 被抢占的窗口。**
你可以把 `sieveLimit` 调小（比如 5000000 → 约 19ms），空白会缩短；
调大到 40000000 → 约 340ms，抢占次数变多，G4 和 G3 的交接会更明显。
**G3 是个很好的"抢占显微镜"**：它计算期间一个字都不打，所以任何插进来的输出都必然来自别人。

### 建议 3：把 `-race` 那次的输出和普通那次并排对比

普通构建：`G5` 第一个跑。
`-race` 构建：`G4` 第一个跑，`G5` 垫底。

**同一个程序，同一份源码，只是加了个 `-race` 编译标志，goroutine 的执行顺序就变了。**
这一条比任何理论讲解都有说服力 —— 它证明了「goroutine 的执行顺序」是**不可依赖**的。
写并发代码时，唯一能依赖的只有 `WaitGroup`（"都跑完了"）和 `channel`（"数据同步了"）。

---

## 9. 附：三个 goroutine 顺序的速查表

| 机制 | 证据（单 P 运行） | 源码位置 |
|---|---|---|
| **runnext LIFO**（最后创建的最先跑） | `main: created G5` 是最后一条创建日志，却是**第一条**输出（`[G5 + 0.063ms]`） | `proc.go` `runqput()` |
| **环形队列 FIFO**（被挤下去的按创建顺序排队） | `G5 → G1 → G2 → G3 → G4`，G1 被 G2 挤出 VIP 槽后落到**队头**，所以第二个跑 | `proc.go` `runqget()` |
| **10ms 时间片抢占** | G3 在 `+0.141ms` 启动，`+154.336ms` 才结束，中间 15ms 的空白里 G4 抢走了 CPU | `proc.go` `forcePreemptNS` / `gopreempt_m` |
| **多 P 真并行** | 默认 8 P 时，G3/G4/G2/G1 的首行挤在 0.09ms 窗口内 = 同时在跑 | `proc.go` `findRunnable()` |
| **`-race` 随机化调度** | `-race` 下第一个跑的是 G4，G5 垫底 | `proc.go` `randomizeScheduler = raceenabled` |

---

*本文所有输出均为本机（go1.27.4 linux/amd64，8 逻辑核）实跑所得，未经编辑。*
*`listing01.go` / `listing04.go` / `listing07.go` 未作任何改动。*
