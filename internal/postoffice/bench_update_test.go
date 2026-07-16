package postoffice

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/renorris/openfsd/internal/session"
)

// BenchmarkUpdatePosition measures geospatial index rewrite cost.
func BenchmarkUpdatePosition(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			p := New()
			r := rand.New(rand.NewSource(42))
			clients := make([]*session.Session, n)
			for i := 0; i < n; i++ {
				c := newTestClient(
					fmt.Sprintf("U%d", i),
					-90+r.Float64()*180,
					-180+r.Float64()*360,
					50*1852,
				)
				if err := p.Register(c); err != nil {
					b.Fatal(err)
				}
				clients[i] = c
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c := clients[i%n]
				lat := -90 + r.Float64()*180
				lon := -180 + r.Float64()*360
				p.UpdatePosition(c, [2]float64{lat, lon}, 50*1852)
			}
		})
	}
}
