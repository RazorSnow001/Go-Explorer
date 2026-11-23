# listing6 race condition 对比实验

## 跑法

### 不带 race detector（看结果）
go run main.go

### 带 race detector（看报告）
go run -race main.go

## 对比实验
把 `// runtime.Gosched()` 那行的注释去掉，再跑一遍，对比两次 FinalCounter 的值和 race detector 的报告。
