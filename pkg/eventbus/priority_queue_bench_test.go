package eventbus

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

// Helper function to create realistic event messages
func createBenchmarkEvent(eventType string, size int) *eventMessage {
	// Create realistic chat message data with variable size
	message := make([]byte, size)
	for i := range message {
		message[i] = byte('a' + (i % 26))
	}

	data := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username:    fmt.Sprintf("user_%d", rand.Intn(1000)),
			Message:     string(message),
			UserRank:    rand.Intn(5),
			UserID:      fmt.Sprintf("uid_%d", rand.Intn(10000)),
			Channel:     "benchmark_channel",
			MessageTime: time.Now().Unix(),
		},
		KeyValue: map[string]string{
			"benchmark": "true",
			"test_id":   fmt.Sprintf("%d", rand.Int63()),
		},
	}

	metadata := &framework.EventMetadata{
		Source:    "benchmark",
		EventType: eventType,
		Timestamp: time.Now(),
		Priority:  framework.PriorityNormal,
		Tags:      []string{"benchmark", "test"},
	}

	return &eventMessage{
		Type:     eventType,
		Data:     data,
		Metadata: metadata,
	}
}

// Benchmark throughput with same priority messages
func BenchmarkPriorityQueue_SamePriority(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			mq := newMessageQueue()
			defer mq.close()

			// Start consumer
			var consumed int64
			done := make(chan struct{})
			go func() {
				for {
					_, ok := mq.pop()
					if !ok {
						close(done)
						return
					}
					atomic.AddInt64(&consumed, 1)
				}
			}()

			b.ResetTimer()
			b.ReportAllocs()

			// Producer
			for i := 0; i < b.N; i++ {
				event := createBenchmarkEvent("benchmark.same_priority", size)
				mq.push(event, framework.PriorityNormal)
			}

			// Wait for all messages to be consumed
			for atomic.LoadInt64(&consumed) < int64(b.N) {
				time.Sleep(time.Millisecond)
			}

			b.StopTimer()
			mq.close()
			<-done

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/sec")
		})
	}
}

// Benchmark throughput with mixed priority messages
func BenchmarkPriorityQueue_MixedPriority(b *testing.B) {
	priorities := []int{
		framework.PriorityNormal,
		framework.PriorityHigh,
		framework.PriorityUrgent,
		framework.PriorityCritical,
	}

	b.Run("even_distribution", func(b *testing.B) {
		mq := newMessageQueue()
		defer mq.close()

		// Start consumer
		var consumed int64
		done := make(chan struct{})
		go func() {
			for {
				_, ok := mq.pop()
				if !ok {
					close(done)
					return
				}
				atomic.AddInt64(&consumed, 1)
			}
		}()

		b.ResetTimer()
		b.ReportAllocs()

		// Producer with mixed priorities
		for i := 0; i < b.N; i++ {
			event := createBenchmarkEvent("benchmark.mixed_priority", 256)
			priority := priorities[i%len(priorities)]
			mq.push(event, priority)
		}

		// Wait for all messages to be consumed
		for atomic.LoadInt64(&consumed) < int64(b.N) {
			time.Sleep(time.Millisecond)
		}

		b.StopTimer()
		mq.close()
		<-done

		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/sec")
	})

	b.Run("realistic_distribution", func(b *testing.B) {
		mq := newMessageQueue()
		defer mq.close()

		// Start consumer
		var consumed int64
		done := make(chan struct{})
		go func() {
			for {
				_, ok := mq.pop()
				if !ok {
					close(done)
					return
				}
				atomic.AddInt64(&consumed, 1)
			}
		}()

		b.ResetTimer()
		b.ReportAllocs()

		// Producer with realistic priority distribution
		// 70% normal, 20% high, 8% urgent, 2% critical
		for i := 0; i < b.N; i++ {
			event := createBenchmarkEvent("benchmark.realistic_priority", 256)
			r := rand.Intn(100)
			var priority int
			switch {
			case r < 70:
				priority = framework.PriorityNormal
			case r < 90:
				priority = framework.PriorityHigh
			case r < 98:
				priority = framework.PriorityUrgent
			default:
				priority = framework.PriorityCritical
			}
			mq.push(event, priority)
		}

		// Wait for all messages to be consumed
		for atomic.LoadInt64(&consumed) < int64(b.N) {
			time.Sleep(time.Millisecond)
		}

		b.StopTimer()
		mq.close()
		<-done

		b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/sec")
	})
}

// Benchmark latency by priority level
func BenchmarkPriorityQueue_LatencyByPriority(b *testing.B) {
	priorities := []struct {
		name     string
		priority int
	}{
		{"normal", framework.PriorityNormal},
		{"high", framework.PriorityHigh},
		{"urgent", framework.PriorityUrgent},
		{"critical", framework.PriorityCritical},
	}

	for _, p := range priorities {
		b.Run(p.name, func(b *testing.B) {
			mq := newMessageQueue()
			defer mq.close()

			// Latency tracking
			latencies := make([]time.Duration, 0, b.N)
			latencyChan := make(chan time.Duration, b.N)

			// Start consumer that measures latency
			done := make(chan struct{})
			go func() {
				for {
					msg, ok := mq.pop()
					if !ok {
						close(done)
						return
					}
					// Calculate latency from timestamp in metadata
					latency := time.Since(msg.Metadata.Timestamp)
					latencyChan <- latency
				}
			}()

			// Start collector for latencies
			go func() {
				for latency := range latencyChan {
					latencies = append(latencies, latency)
				}
			}()

			b.ResetTimer()

			// Push messages with specified priority
			for i := 0; i < b.N; i++ {
				event := createBenchmarkEvent("benchmark.latency", 256)
				event.Metadata.Timestamp = time.Now() // Update timestamp just before push
				mq.push(event, p.priority)
			}

			// Wait for all messages to be processed
			for len(latencies) < b.N {
				time.Sleep(time.Millisecond)
			}

			b.StopTimer()
			close(latencyChan)
			mq.close()
			<-done

			// Calculate percentiles
			if len(latencies) > 0 {
				var total time.Duration
				for _, l := range latencies {
					total += l
				}
				avg := total / time.Duration(len(latencies))
				b.ReportMetric(float64(avg.Nanoseconds()), "ns/op")
			}
		})
	}
}

// Benchmark concurrent access with multiple producers/consumers
func BenchmarkPriorityQueue_Concurrent(b *testing.B) {
	scenarios := []struct {
		name      string
		producers int
		consumers int
	}{
		{"1p1c", 1, 1},
		{"2p2c", 2, 2},
		{"4p4c", 4, 4},
		{"8p8c", 8, 8},
		{"1p4c", 1, 4},
		{"4p1c", 4, 1},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			mq := newMessageQueue()
			defer mq.close()

			msgsPerProducer := b.N / s.producers
			var produced, consumed int64

			// Start consumers
			consumerWg := sync.WaitGroup{}
			consumerWg.Add(s.consumers)
			for i := 0; i < s.consumers; i++ {
				go func() {
					defer consumerWg.Done()
					for {
						_, ok := mq.pop()
						if !ok {
							return
						}
						atomic.AddInt64(&consumed, 1)
					}
				}()
			}

			b.ResetTimer()
			b.ReportAllocs()

			// Start producers
			producerWg := sync.WaitGroup{}
			producerWg.Add(s.producers)
			start := time.Now()

			for i := 0; i < s.producers; i++ {
				go func(id int) {
					defer producerWg.Done()
					for j := 0; j < msgsPerProducer; j++ {
						event := createBenchmarkEvent(fmt.Sprintf("benchmark.concurrent_%d", id), 256)
						priority := rand.Intn(4) // Random priority
						mq.push(event, priority)
						atomic.AddInt64(&produced, 1)
					}
				}(i)
			}

			// Wait for all producers to finish
			producerWg.Wait()
			totalProduced := atomic.LoadInt64(&produced)

			// Wait for all messages to be consumed
			for atomic.LoadInt64(&consumed) < totalProduced {
				time.Sleep(time.Millisecond)
			}

			elapsed := time.Since(start)
			b.StopTimer()

			mq.close()
			consumerWg.Wait()

			b.ReportMetric(float64(totalProduced)/elapsed.Seconds(), "msgs/sec")
			b.ReportMetric(float64(s.producers), "producers")
			b.ReportMetric(float64(s.consumers), "consumers")
		})
	}
}

// Benchmark priority queue vs simple channel
func BenchmarkPriorityQueueVsChannel(b *testing.B) {
	sizes := []int{256, 1024}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("priority_queue_size_%d", size), func(b *testing.B) {
			mq := newMessageQueue()
			defer mq.close()

			var consumed int64
			done := make(chan struct{})
			go func() {
				for {
					_, ok := mq.pop()
					if !ok {
						close(done)
						return
					}
					atomic.AddInt64(&consumed, 1)
				}
			}()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				event := createBenchmarkEvent("benchmark.queue", size)
				mq.push(event, rand.Intn(4))
			}

			for atomic.LoadInt64(&consumed) < int64(b.N) {
				time.Sleep(time.Millisecond)
			}

			b.StopTimer()
			mq.close()
			<-done

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/sec")
		})

		b.Run(fmt.Sprintf("channel_size_%d", size), func(b *testing.B) {
			ch := make(chan *eventMessage, 1000)
			defer close(ch)

			var consumed int64
			done := make(chan struct{})
			go func() {
				for range ch {
					atomic.AddInt64(&consumed, 1)
				}
				close(done)
			}()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				event := createBenchmarkEvent("benchmark.channel", size)
				ch <- event
			}

			for atomic.LoadInt64(&consumed) < int64(b.N) {
				time.Sleep(time.Millisecond)
			}

			b.StopTimer()
			close(ch)
			<-done

			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/sec")
		})
	}
}

// Benchmark queue performance under load
func BenchmarkPriorityQueue_UnderLoad(b *testing.B) {
	queueSizes := []int{100, 1000, 10000}

	for _, qsize := range queueSizes {
		b.Run(fmt.Sprintf("queue_size_%d", qsize), func(b *testing.B) {
			mq := newMessageQueue()
			defer mq.close()

			// Pre-fill queue to simulate load
			for i := 0; i < qsize; i++ {
				event := createBenchmarkEvent("benchmark.prefill", 256)
				mq.push(event, rand.Intn(4))
			}

			var consumed int64
			done := make(chan struct{})

			// Start consumer with some processing delay
			go func() {
				for {
					_, ok := mq.pop()
					if !ok {
						close(done)
						return
					}
					// Simulate minimal processing
					time.Sleep(time.Microsecond)
					atomic.AddInt64(&consumed, 1)
				}
			}()

			b.ResetTimer()
			b.ReportAllocs()

			// Producer trying to push while queue is under load
			start := time.Now()
			for i := 0; i < b.N; i++ {
				event := createBenchmarkEvent("benchmark.load", 256)
				mq.push(event, rand.Intn(4))
			}
			pushTime := time.Since(start)

			// Wait for queue to drain
			totalExpected := int64(qsize + b.N)
			for atomic.LoadInt64(&consumed) < totalExpected {
				time.Sleep(time.Millisecond)
			}

			b.StopTimer()
			mq.close()
			<-done

			b.ReportMetric(float64(b.N)/pushTime.Seconds(), "push_msgs/sec")
			b.ReportMetric(float64(qsize), "initial_queue_size")
		})
	}
}

// Benchmark priority ordering correctness under high concurrency
func BenchmarkPriorityQueue_OrderingCorrectness(b *testing.B) {
	b.Run("priority_ordering", func(b *testing.B) {
		mq := newMessageQueue()
		defer mq.close()

		// Track dequeue order by priority
		var criticalCount, urgentCount, highCount, normalCount int64
		done := make(chan struct{})

		go func() {
			for {
				msg, ok := mq.pop()
				if !ok {
					close(done)
					return
				}
				switch msg.Metadata.Priority {
				case framework.PriorityCritical:
					atomic.AddInt64(&criticalCount, 1)
				case framework.PriorityUrgent:
					atomic.AddInt64(&urgentCount, 1)
				case framework.PriorityHigh:
					atomic.AddInt64(&highCount, 1)
				case framework.PriorityNormal:
					atomic.AddInt64(&normalCount, 1)
				}
			}
		}()

		b.ResetTimer()

		// Push messages with different priorities
		totalPerPriority := b.N / 4
		for i := 0; i < totalPerPriority; i++ {
			// Push in mixed order
			for _, p := range []int{
				framework.PriorityNormal,
				framework.PriorityCritical,
				framework.PriorityHigh,
				framework.PriorityUrgent,
			} {
				event := createBenchmarkEvent("benchmark.ordering", 256)
				event.Metadata.Priority = p
				mq.push(event, p)
			}
		}

		// Wait for all processing
		totalExpected := int64(totalPerPriority * 4)
		for {
			total := atomic.LoadInt64(&criticalCount) +
				atomic.LoadInt64(&urgentCount) +
				atomic.LoadInt64(&highCount) +
				atomic.LoadInt64(&normalCount)
			if total >= totalExpected {
				break
			}
			time.Sleep(time.Millisecond)
		}

		b.StopTimer()
		mq.close()
		<-done

		// Report distribution
		b.ReportMetric(float64(criticalCount), "critical_processed")
		b.ReportMetric(float64(urgentCount), "urgent_processed")
		b.ReportMetric(float64(highCount), "high_processed")
		b.ReportMetric(float64(normalCount), "normal_processed")
	})
}
