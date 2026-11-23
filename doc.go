// This sample program demonstrates how to create goroutines and
// how the scheduler behaves.
//
// The three example programs in this directory (listing01.go, listing04.go and
// listing07.go) each declare their own `func main`, so they cannot live in one
// compiled package. Each file therefore carries a `//go:build ignore`
// constraint, which is the conventional Go marker for "this file is a
// standalone example — build it on its own".
//
// Consequences:
//
//	go run listing01.go   → runs that one example
//	go vet listing01.go   → vets that one example (constraints are not applied
//	                        to files named explicitly on the command line)
//	go build ./...        → builds this package only; the examples are skipped
//	go vet ./...          → same
//
// The example bodies themselves are copied verbatim from "Go in Action"
// (2nd edition), Chapter 6 — only the build constraint line was added.
package main

import "fmt"

func main() {
	fmt.Println("Go in Action — Chapter 6.2 (Goroutines) examples")
	fmt.Println()
	fmt.Println("Run one example at a time (each has its own func main):")
	fmt.Println("  go run listing01.go   # Listing 6.1, GOMAXPROCS(1) — 并发(单 P 时间片)")
	fmt.Println("  go run listing07.go   # Listing 6.7, GOMAXPROCS(2) — 并行(两个 P)")
	fmt.Println("  go run listing04.go   # Listing 6.4, 质数, 观察调度器切换")
	fmt.Println("  go run exp_goroutine_order.go   # 调度顺序实验(GOMAXPROCS/GOROUTINES/WORK_MS 环境变量控制)")
	fmt.Println()
	fmt.Println("Useful extras:")
	fmt.Println("  go run -race listing01.go   # 竞态检测器(本例 0 条 DATA RACE)")
	fmt.Println("  go vet ./...                # 静态检查")
	fmt.Println("  gofmt -l .                  # 列出格式不合规的文件")
	fmt.Println()
	fmt.Println("See README.md for the page mapping, a line-by-line walkthrough,")
	fmt.Println("11 hands-on experiments and the common pitfalls.")
}
