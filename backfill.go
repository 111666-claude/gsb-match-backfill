// Package backfill 是补人队列：给对局空位挑候补，按评级区间筛选、先到先得。
// 缺陷：不看评级区间、不看到期时间、一次把池子全取走、同一玩家重复入队、
// 每次挑选与查询全扫候选池。
package backfill

type candidate struct {
	player string
	rating int
	atMs   int
}

// Pool 是补人候选池。
type Pool struct {
	ttlMs   int
	queue   []candidate
	scanned int
}

// New 建候选池：ttlMs 是排队有效期。
func New(ttlMs int) *Pool { return &Pool{ttlMs: ttlMs} }

// Join 让玩家进入候选池，返回是否成功。缺陷：同一玩家重复入队也照收。
func (p *Pool) Join(player string, rating, atMs int) bool {
	p.queue = append(p.queue, candidate{player: player, rating: rating, atMs: atMs})
	return true
}

// Fill 给 need 个空位挑人，按入队顺序返回挑中的玩家。
// 缺陷：不筛评级区间、不看 need、不清过期、全扫候选池。
func (p *Pool) Fill(need, minRating, maxRating, nowMs int) []string {
	p.scanned += len(p.queue)
	out := []string{}
	for _, item := range p.queue {
		out = append(out, item.player)
	}
	p.queue = nil
	return out
}

// Pending 返回池中还在排队的候选人数。缺陷：过期的人也算、全扫候选池。
func (p *Pool) Pending(nowMs int) int {
	p.scanned += len(p.queue)
	return len(p.queue)
}

// Scanned 是累计扫描的候选人数（规模观测）。
func (p *Pool) Scanned() int { return p.scanned }
