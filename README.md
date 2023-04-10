# cache

cache is like a Go sync.Map but with generic is safe for concurrent use 
by multiple goroutines without additional locking or coordination.
Loads, stores, and deletes run in amortized constant time.

### Installation

`go get github.com/weiwenchen2022/cache`
