#!/bin/bash

# Script to run priority queue benchmarks with various configurations

echo "====================================="
echo "Priority Queue Benchmark Suite"
echo "====================================="
echo

# Run all benchmarks with short duration for quick overview
echo "Running quick benchmark overview (1s each)..."
echo
go test -bench=. -benchtime=1s -benchmem ./pkg/eventbus | grep -E "Benchmark|msgs/sec|ns/op"

echo
echo "====================================="
echo "Detailed benchmark results"
echo "====================================="
echo

# Run specific benchmarks with longer duration for accurate results
benchmarks=(
    "BenchmarkPriorityQueue_SamePriority"
    "BenchmarkPriorityQueue_MixedPriority"
    "BenchmarkPriorityQueue_LatencyByPriority"
    "BenchmarkPriorityQueue_Concurrent"
    "BenchmarkPriorityQueueVsChannel"
    "BenchmarkPriorityQueue_UnderLoad"
    "BenchmarkPriorityQueue_OrderingCorrectness"
)

for bench in "${benchmarks[@]}"; do
    echo "Running $bench..."
    go test -bench="^${bench}$" -benchtime=5s -benchmem ./pkg/eventbus 2>/dev/null | grep -E "Benchmark|msgs/sec|ns/op|allocs/op"
    echo
done

echo "====================================="
echo "Benchmark Summary"
echo "====================================="
echo
echo "Key metrics:"
echo "- msgs/sec: Throughput (higher is better)"
echo "- ns/op: Latency per operation (lower is better)"
echo "- B/op: Bytes allocated per operation (lower is better)"
echo "- allocs/op: Number of allocations per operation (lower is better)"
echo
echo "The benchmarks test:"
echo "1. Throughput with same priority vs mixed priorities"
echo "2. Latency by priority level (critical should be fastest)"
echo "3. Concurrent access with various producer/consumer ratios"
echo "4. Performance comparison with simple channels"
echo "5. Performance under load with pre-filled queues"
echo "6. Priority ordering correctness under concurrency"