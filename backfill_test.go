package backfill

import (
	"strconv"
	"testing"
)

func TestEmptyPoolPicksNobody(t *testing.T) {
	if got := New(5000).Fill(1, 0, 9999, 0); len(got) != 0 {
		t.Fatalf("空池不该挑出人，得到 %v", got)
	}
}

func TestJoinThenFill(t *testing.T) {
	pool := New(5000)
	if !pool.Join("a", 1500, 0) {
		t.Fatal("入队应该成功")
	}
	if got := pool.Fill(1, 0, 9999, 0); len(got) != 1 || got[0] != "a" {
		t.Fatalf("池里唯一的人应该被挑中，得到 %v", got)
	}
}

func TestPendingCountsCandidates(t *testing.T) {
	pool := New(5000)
	pool.Join("a", 1500, 0)
	pool.Join("b", 1600, 0)
	if got := pool.Pending(0); got != 2 {
		t.Fatalf("池中应该有两个候选，得到 %d", got)
	}
}

func TestScannedStartsAtZero(t *testing.T) {
	if New(5000).Scanned() != 0 {
		t.Fatal("Scanned 初始应该是 0")
	}
}

func TestFillRespectsRatingRange(t *testing.T) {
	pool := New(100000)
	pool.Join("low", 999, 0)
	pool.Join("min", 1000, 1)
	pool.Join("mid", 1500, 2)
	pool.Join("max", 2000, 3)
	pool.Join("high", 2001, 4)
	got := pool.Fill(10, 1000, 2000, 10)
	if len(got) != 3 || got[0] != "min" || got[1] != "mid" || got[2] != "max" {
		t.Fatalf("只该挑中区间（含边界）内的人并按入队升序，得到 %v", got)
	}
	if pending := pool.Pending(10); pending != 2 {
		t.Fatalf("区间外的两人应留在池中，得到 %d", pending)
	}
}

func TestFillCapsAtNeed(t *testing.T) {
	pool := New(100000)
	for index := 0; index < 5; index++ {
		pool.Join(string(rune('a'+index)), 1500, index)
	}
	got := pool.Fill(2, 1000, 2000, 10)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("只该挑两人且先到先得，得到 %v", got)
	}
	if pending := pool.Pending(10); pending != 3 {
		t.Fatalf("挑走两人后应剩三人，得到 %d", pending)
	}
}

func TestFillFirstComeFirstServed(t *testing.T) {
	pool := New(100000)
	pool.Join("late", 1500, 20)
	pool.Join("early", 1500, 5)
	pool.Join("middle", 1500, 10)
	got := pool.Fill(3, 0, 9999, 30)
	if len(got) != 3 || got[0] != "early" || got[1] != "middle" || got[2] != "late" {
		t.Fatalf("应按入队时间升序挑人，得到 %v", got)
	}
}

func TestExpiryEvictsFromPendingAndFill(t *testing.T) {
	pool := New(5000)
	pool.Join("stale", 1500, 0)
	pool.Join("fresh", 1500, 8000)
	if pending := pool.Pending(5000); pending != 1 {
		t.Fatalf("入队满 ttlMs 的候选应出池，得到 %d", pending)
	}
	got := pool.Fill(2, 0, 9999, 5000)
	if len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("过期候选不该被挑中，得到 %v", got)
	}
	if pending := pool.Pending(5000); pending != 0 {
		t.Fatalf("挑走后池应空，得到 %d", pending)
	}
}

func TestExpiryBoundaryKeepsYounger(t *testing.T) {
	pool := New(5000)
	pool.Join("edge", 1500, 1)
	if pending := pool.Pending(5000); pending != 1 {
		t.Fatalf("距 nowMs 未达 ttlMs 的候选应保留，得到 %d", pending)
	}
	if pending := pool.Pending(5001); pending != 0 {
		t.Fatalf("距 nowMs 达到 ttlMs 的候选应出池，得到 %d", pending)
	}
}

func TestJoinDuplicateIsRejected(t *testing.T) {
	pool := New(100000)
	if !pool.Join("a", 1500, 0) {
		t.Fatal("首次入队应成功")
	}
	if pool.Join("a", 9999, 50) {
		t.Fatal("重复入队应返回 false")
	}
	if pending := pool.Pending(60); pending != 1 {
		t.Fatalf("重复入队不该占位，得到 %d", pending)
	}
	got := pool.Fill(1, 0, 2000, 60)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("原有候选应保持原评级并被挑中，得到 %v", got)
	}
	if !pool.Join("a", 9999, 70) {
		t.Fatal("挑中出池后同名玩家应可重新入队")
	}
	if pending := pool.Pending(70); pending != 1 {
		t.Fatalf("重新入队后池中应有一人，得到 %d", pending)
	}
	if got := pool.Fill(1, 0, 2000, 80); len(got) != 0 {
		t.Fatalf("重新入队的评级在区间外不该被挑中，得到 %v", got)
	}
}

func TestPickedNotLeftInPool(t *testing.T) {
	pool := New(100000)
	pool.Join("a", 1500, 0)
	pool.Join("b", 1500, 1)
	pool.Fill(1, 0, 9999, 2)
	got := pool.Fill(1, 0, 9999, 2)
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("被挑中的不该再被挑中，得到 %v", got)
	}
	if pending := pool.Pending(2); pending != 0 {
		t.Fatalf("被挑中的不该留在池中，得到 %d", pending)
	}
}

func TestReplayIsDeterministic(t *testing.T) {
	run := func() []string {
		pool := New(7000)
		for index := 0; index < 200; index++ {
			pool.Join("p-"+itoa(index), 1000+index*7%1500, index*3)
		}
		out := []string{}
		for round := 0; round < 20; round++ {
			out = append(out, pool.Fill(3, 1200, 1800, round*25)...)
		}
		return out
	}
	first := run()
	second := run()
	if len(first) != len(second) {
		t.Fatalf("重放长度不一致：%d 对 %d", len(first), len(second))
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("重放结果不一致：第 %d 位 %q 对 %q", index, first[index], second[index])
		}
	}
}

func TestScannedDoesNotScaleWithPoolTimesCalls(t *testing.T) {
	pool := New(1000000)
	for index := 0; index < 3000; index++ {
		pool.Join("p-"+itoa(index), 1500, 0)
	}
	for index := 0; index < 3000; index++ {
		pool.Pending(0)
	}
	for index := 0; index < 3000; index++ {
		pool.Fill(1, 1000, 2000, 0)
	}
	if scanned := pool.Scanned(); scanned > 6000 {
		t.Fatalf("Scanned 不应随池大小乘调用次数增长，得到 %d", scanned)
	}
}

func TestScaleFillStaysFast(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过规模测试")
	}
	pool := New(1000000)
	for index := 0; index < 200000; index++ {
		if !pool.Join("p-"+itoa(index), 1000+index%1000, index) {
			t.Fatal("入队应全部成功")
		}
	}
	for index := 0; index < 100000; index++ {
		if got := pool.Fill(1, 1000, 1999, index); len(got) != 1 {
			t.Fatalf("第 %d 次挑选应挑中一人", index)
		}
	}
	if pending := pool.Pending(100000); pending != 100000 {
		t.Fatalf("挑走一半后应剩十万人，得到 %d", pending)
	}
}

func itoa(index int) string { return strconv.Itoa(index) }
