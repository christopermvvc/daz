# Priority Queue Benchmarks

This document describes the comprehensive benchmark suite for the EventBus priority queue implementation.

## Benchmark Overview

The benchmark suite tests various aspects of the priority queue performance:

### 1. `BenchmarkPriorityQueue_SamePriority`
Tests throughput when all messages have the same priority level. This represents the baseline performance without priority-based reordering.

- **Message sizes tested**: 64, 256, 1024, 4096 bytes
- **Metrics**: messages/second, allocations per operation

### 2. `BenchmarkPriorityQueue_MixedPriority`
Tests throughput with mixed priority messages in two scenarios:
- **Even distribution**: Equal number of messages at each priority level
- **Realistic distribution**: 70% normal, 20% high, 8% urgent, 2% critical

### 3. `BenchmarkPriorityQueue_LatencyByPriority`
Measures the latency from enqueue to dequeue for each priority level:
- Normal (0)
- High (1)
- Urgent (2)
- Critical (3)

### 4. `BenchmarkPriorityQueue_Concurrent`
Tests concurrent access patterns with various producer/consumer configurations:
- 1 producer, 1 consumer (1p1c)
- 2 producers, 2 consumers (2p2c)
- 4 producers, 4 consumers (4p4c)
- 8 producers, 8 consumers (8p8c)
- 1 producer, 4 consumers (1p4c)
- 4 producers, 1 consumer (4p1c)

### 5. `BenchmarkPriorityQueueVsChannel`
Compares the performance of the priority queue against a simple Go channel to understand the overhead of priority management.

### 6. `BenchmarkPriorityQueue_UnderLoad`
Tests performance when the queue is pre-filled with messages, simulating real-world scenarios where the queue may have a backlog.

### 7. `BenchmarkPriorityQueue_OrderingCorrectness`
Verifies that priority ordering is maintained correctly under high concurrency.

## Running the Benchmarks

### Quick Test
```bash
go test -bench=BenchmarkPriorityQueue_SamePriority -benchtime=1s ./pkg/eventbus
```

### Full Suite
```bash
./scripts/run-priority-queue-benchmarks.sh
```

### Specific Benchmark
```bash
go test -bench=BenchmarkPriorityQueue_Concurrent -benchtime=10s -benchmem ./pkg/eventbus
```

## Interpreting Results

### Key Metrics
- **msgs/sec**: Throughput - higher is better
- **ns/op**: Nanoseconds per operation - lower is better
- **B/op**: Bytes allocated per operation - lower is better
- **allocs/op**: Number of allocations per operation - lower is better

### Expected Performance Characteristics

1. **Priority Overhead**: The priority queue should show some overhead compared to a simple channel due to heap operations.

2. **Latency by Priority**: Critical messages should have the lowest latency, followed by urgent, high, and normal.

3. **Concurrent Scaling**: Performance should scale well with multiple producers/consumers, though contention may limit scaling at high concurrency.

4. **Memory Efficiency**: Each message should have consistent allocation patterns regardless of priority.

## Sample Results

```
BenchmarkPriorityQueue_SamePriority/size_256-16    1264451 msgs/sec    987.1 ns/op    1471 B/op    15 allocs/op
BenchmarkPriorityQueueVsChannel/channel_size_256-16    2847633 msgs/sec    351.2 ns/op    256 B/op    1 allocs/op
```

This shows the priority queue achieves ~1.26M messages/second with 256-byte messages, while a simple channel achieves ~2.85M messages/second. The ~56% overhead is reasonable for the added priority management capabilities.

## Optimization Opportunities

Based on benchmark results, consider:

1. **Batch Processing**: Process multiple messages per lock acquisition
2. **Lock-Free Structures**: For specific high-throughput scenarios
3. **Priority Buckets**: Separate queues per priority level
4. **Memory Pooling**: Reuse message allocations