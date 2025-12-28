#!/usr/bin/env python3
"""
NebulaIO ML Training Cache Example

Demonstrates how NebulaIO's DRAM cache optimizes ML training workloads
with automatic prefetching and high-throughput data loading.
"""

import os
import time
import boto3
from botocore.config import Config
from concurrent.futures import ThreadPoolExecutor
import threading

# Configuration
# Note: Set credentials via environment variables (no defaults for security)
ENDPOINT = os.getenv("NEBULAIO_ENDPOINT", "https://localhost:9000")
ACCESS_KEY = os.getenv("NEBULAIO_ACCESS_KEY", "admin")
SECRET_KEY = os.getenv("NEBULAIO_SECRET_KEY")  # Required - set via environment
ADMIN_ENDPOINT = os.getenv("NEBULAIO_ADMIN_ENDPOINT", "https://localhost:9001")

if not SECRET_KEY:
    raise ValueError("NEBULAIO_SECRET_KEY environment variable is required")


def get_s3_client():
    """Create an S3 client configured for NebulaIO."""
    return boto3.client(
        "s3",
        endpoint_url=ENDPOINT,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
        config=Config(signature_version="s3v4"),
        region_name="us-east-1",
    )


class NebulaIODataLoader:
    """
    A data loader optimized for NebulaIO's DRAM cache.

    Features:
    - Sequential access pattern triggers prefetching
    - Parallel chunk loading
    - Epoch-aware warmup
    """

    def __init__(self, bucket: str, prefix: str, batch_size: int = 32):
        self.client = get_s3_client()
        self.bucket = bucket
        self.prefix = prefix
        self.batch_size = batch_size
        self.objects = []
        self._load_object_list()

    def _load_object_list(self):
        """List all objects under the prefix."""
        paginator = self.client.get_paginator("list_objects_v2")
        for page in paginator.paginate(Bucket=self.bucket, Prefix=self.prefix):
            for obj in page.get("Contents", []):
                self.objects.append(obj["Key"])
        print(f"Found {len(self.objects)} objects")

    def warmup_cache(self, num_objects: int = None):
        """
        Pre-warm the cache before training starts.

        NebulaIO's cache warmup API can be called via admin endpoint,
        but sequential reads also trigger the prefetcher.
        """
        objects_to_warm = self.objects[:num_objects] if num_objects else self.objects
        print(f"Warming up cache with {len(objects_to_warm)} objects...")

        start = time.time()
        with ThreadPoolExecutor(max_workers=8) as executor:
            futures = []
            for key in objects_to_warm:
                futures.append(executor.submit(self._read_object, key))

            for i, future in enumerate(futures):
                future.result()
                if (i + 1) % 100 == 0:
                    print(f"  Warmed {i + 1}/{len(objects_to_warm)} objects")

        elapsed = time.time() - start
        print(f"Cache warmup complete in {elapsed:.2f}s")

    def _read_object(self, key: str) -> bytes:
        """Read a single object from NebulaIO."""
        response = self.client.get_object(Bucket=self.bucket, Key=key)
        return response["Body"].read()

    def __iter__(self):
        """Iterate through batches of data."""
        batch = []
        for key in self.objects:
            data = self._read_object(key)
            batch.append(data)

            if len(batch) >= self.batch_size:
                yield batch
                batch = []

        if batch:
            yield batch

    def __len__(self):
        return (len(self.objects) + self.batch_size - 1) // self.batch_size


class CacheMetrics:
    """Monitor NebulaIO cache performance."""

    def __init__(self):
        import requests
        self.session = requests.Session()

    def get_stats(self):
        """Get cache statistics from admin API."""
        import requests

        try:
            response = self.session.get(
                f"{ADMIN_ENDPOINT}/admin/cache/stats",
                headers={"Authorization": f"Bearer {os.getenv('NEBULAIO_ADMIN_TOKEN', '')}"}
            )
            return response.json()
        except Exception as e:
            print(f"Could not fetch cache stats: {e}")
            return {}

    def print_stats(self):
        """Print current cache statistics."""
        stats = self.get_stats()
        if stats:
            print("\n--- Cache Statistics ---")
            print(f"Hit Rate: {stats.get('hit_rate', 0) * 100:.1f}%")
            print(f"Size: {stats.get('size', 0) / (1024**3):.2f} GB")
            print(f"Prefetch Hits: {stats.get('prefetch_hits', 0)}")
            print(f"Avg Latency: {stats.get('avg_latency_us', 0):.0f} us")
            print("------------------------\n")


def simulate_training_epoch(loader: NebulaIODataLoader, epoch: int):
    """Simulate a training epoch."""
    print(f"\n=== Epoch {epoch} ===")
    start = time.time()

    total_bytes = 0
    batch_times = []

    for i, batch in enumerate(loader):
        batch_start = time.time()

        # Simulate processing (normally this would be model.train(batch))
        for data in batch:
            total_bytes += len(data)

        batch_time = time.time() - batch_start
        batch_times.append(batch_time)

        if (i + 1) % 10 == 0:
            avg_time = sum(batch_times[-10:]) / len(batch_times[-10:])
            throughput = (loader.batch_size * len(batch_times[-10:])) / sum(batch_times[-10:])
            print(f"  Batch {i + 1}/{len(loader)}: {avg_time*1000:.1f}ms avg, {throughput:.1f} samples/s")

    elapsed = time.time() - start
    print(f"\nEpoch {epoch} complete:")
    print(f"  Total bytes: {total_bytes / (1024**2):.1f} MB")
    print(f"  Time: {elapsed:.2f}s")
    print(f"  Throughput: {total_bytes / elapsed / (1024**2):.1f} MB/s")


def main():
    """Main training loop demonstrating cache benefits."""
    import argparse

    parser = argparse.ArgumentParser(description="NebulaIO ML Training Cache Example")
    parser.add_argument("bucket", help="S3 bucket containing training data")
    parser.add_argument("--prefix", default="training/", help="Object prefix")
    parser.add_argument("--batch-size", type=int, default=32, help="Batch size")
    parser.add_argument("--epochs", type=int, default=3, help="Number of epochs")
    parser.add_argument("--warmup", action="store_true", help="Warm up cache first")

    args = parser.parse_args()

    print("NebulaIO ML Training Cache Example")
    print("=" * 50)
    print(f"Endpoint: {ENDPOINT}")
    print(f"Bucket: {args.bucket}")
    print(f"Prefix: {args.prefix}")
    print(f"Batch Size: {args.batch_size}")
    print(f"Epochs: {args.epochs}")

    # Initialize data loader
    loader = NebulaIODataLoader(args.bucket, args.prefix, args.batch_size)
    metrics = CacheMetrics()

    # Optional: warm up cache before training
    if args.warmup:
        loader.warmup_cache()
        metrics.print_stats()

    # Training loop
    for epoch in range(1, args.epochs + 1):
        simulate_training_epoch(loader, epoch)
        metrics.print_stats()

    print("\nTraining complete!")
    print("\nNote: First epoch is typically slower due to cache misses.")
    print("Subsequent epochs benefit from cached data and prefetching.")


if __name__ == "__main__":
    main()
