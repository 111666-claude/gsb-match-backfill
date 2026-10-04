# match-backfill

补人队列：给对局空位挑候补，按评级区间筛选、先到先得，只用 Go 标准库。

```
go run ./cmd/match-backfill --sample=rating
go run ./cmd/match-backfill --sample=need
go run ./cmd/match-backfill --sample=ttl
go run ./cmd/match-backfill --sample=dup
go run ./cmd/match-backfill --sample=scan
go test ./...
go vet ./...
```

## 口径（README 为准）

- **评级区间**：只有评级落在 `[minRating, maxRating]` 内的候选才会被挑中，正好等于边界算在内。
- **先到先得**：按入队时间升序挑人，最多挑 `need` 个，挑中的从池中移除。
- **过期清理**：入队时间距 `nowMs` 达到 `ttlMs` 的候选出池，`Pending` 不数它们，也不会被挑中。
- **幂等**：同一个玩家已经在池中就返回 false，不改变他的入队时间与评级。
- **不变量**：池中不重复；被挑中的不再留在池中；挑中的人评级都在区间内；同一串操作重复执行结果相同。
- **代价**：`Fill` 与 `Pending` 不许全扫候选池，`Scanned` 不随池大小乘调用次数增长；
  20 万候选、每秒 10 万次挑选。

## 输出契约（不改格式）

```
picked=..
second=.. pending=..
scanned=..
```

## 目录

```
backfill.go            评级区间、先到先得、过期与幂等
backfill_test.go       单元测试
cmd/match-backfill/    命令行入口
```
