package framework

import (
	"math/rand"
	"sync"
	"time"
)

var mathRandSeedOnce sync.Once

func SeedMathRand() {
	mathRandSeedOnce.Do(func() {
		rand.Seed(time.Now().UnixNano())
	})
}
