package server

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

)

// These wrappers are the only affinity API adaptation installed in the pinned
// release snapshot: its get/set calls receive time.Now explicitly. Production
// implementations and cache capacity/expiry behavior are left unchanged.
func reviewAffinityGet(cache *affinityCache, key string) string {
	return cache.get(key, "group")
}

func reviewAffinitySet(cache *affinityCache, key string) {
	cache.set(key, "group", "model")
}

func BenchmarkReviewCacheAffinity(b *testing.B) {
	for _, size := range []int{64, 4096} {
		for _, mixed := range []bool{false, true} {
			b.Run(fmt.Sprintf("keys=%d/write10=%t", size, mixed), func(b *testing.B) {
				cache := newAffinityCache()
				if closer, ok := any(cache).(interface{ close() }); ok {
					b.Cleanup(closer.close)
				}
				keys := make([]string, size)
				for i := range keys {
					keys[i] = fmt.Sprintf("synthetic-key-%d", i)
					reviewAffinitySet(cache, keys[i])
				}
				var workers atomic.Uint64
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					index := int(workers.Add(1))
					for pb.Next() {
						key := keys[index%size]
						if mixed && index%10 == 0 {
							reviewAffinitySet(cache, key)
						} else if reviewAffinityGet(cache, key) != "model" {
							b.Error("unexpected affinity miss")
						}
						index++
					}
				})
			})
		}
	}
}



// Explanatory lower-bound experiment, not a production redactor replacement.
// The fallback regexp requires a literal quote. Nested JSON/SSE dispatch and
// security differential coverage still belong to a future implementation.
func BenchmarkReviewTurnStateScan(b *testing.B) {
	for _, size := range []int{2, 31} {
		text := strings.Repeat("A", size<<20)
		for _, prefilter := range []bool{false, true} {
			b.Run(fmt.Sprintf("MiB=%d/prefilter=%t", size, prefilter), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(text)))
				for b.Loop() {
					if prefilter && strings.IndexByte(text, '"') < 0 {
						continue
					}
					if partialTurnStateKey.FindStringIndex(text) != nil {
						b.Fatal("synthetic base64 unexpectedly matched")
					}
				}
			})
		}
	}
}
