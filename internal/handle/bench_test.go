package handle

import "testing"

// BenchmarkNewGetTake measures a serial register, lookup and removal cycle,
// which is the shape of a callback that runs on one goroutine.
func BenchmarkNewGetTake(b *testing.B) {
	before := Len()
	value := new(int)
	b.ReportAllocs()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := New(value)
		if _, ok := Get(id); !ok {
			b.Fatalf("Get(%d) missed a registered handle", id)
		}
		if _, ok := Take(id); !ok {
			b.Fatalf("Take(%d) missed a registered handle", id)
		}
	}

	if after := Len(); after != before {
		b.Fatalf("remaining handles = %d, want %d", after, before)
	}
}

// BenchmarkParallelNewGetTake measures register, lookup and removal from many
// goroutines at once, exercising shard contention rather than the lock-free
// identifier counter alone.
func BenchmarkParallelNewGetTake(b *testing.B) {
	before := Len()
	b.ReportAllocs()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		value := new(int)
		for pb.Next() {
			id := New(value)
			if _, ok := Get(id); !ok {
				b.Errorf("Get(%d) missed a registered handle", id)
				return
			}
			if _, ok := Take(id); !ok {
				b.Errorf("Take(%d) missed a registered handle", id)
				return
			}
		}
	})

	if after := Len(); after != before {
		b.Fatalf("remaining handles = %d, want %d", after, before)
	}
}
