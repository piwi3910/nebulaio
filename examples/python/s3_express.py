#!/usr/bin/env python3
"""
S3 Express One Zone Example

Demonstrates ultra-low latency storage operations with atomic appends.
"""

import os
import boto3
from botocore.config import Config

# Configuration
ENDPOINT = os.getenv("NEBULAIO_ENDPOINT", "http://localhost:9000")
ACCESS_KEY = os.getenv("NEBULAIO_ACCESS_KEY", "minioadmin")
SECRET_KEY = os.getenv("NEBULAIO_SECRET_KEY", "minioadmin")

# Create S3 client with retries disabled for low latency
s3 = boto3.client(
    "s3",
    endpoint_url=ENDPOINT,
    aws_access_key_id=ACCESS_KEY,
    aws_secret_access_key=SECRET_KEY,
    config=Config(
        retries={"max_attempts": 1},
        connect_timeout=1,
        read_timeout=5,
    ),
)


def create_express_bucket(bucket_name: str, zone: str = "use1-az1"):
    """Create an S3 Express directory bucket."""
    # Express bucket names must follow pattern: bucket--zone--x-s3
    express_bucket = f"{bucket_name}--{zone}--x-s3"

    try:
        s3.create_bucket(
            Bucket=express_bucket,
            CreateBucketConfiguration={
                "LocationConstraint": zone,
                "Location": {"Type": "AvailabilityZone", "Name": zone},
                "Bucket": {"DataRedundancy": "SingleAvailabilityZone", "Type": "Directory"},
            },
        )
        print(f"Created Express bucket: {express_bucket}")
        return express_bucket
    except s3.exceptions.BucketAlreadyOwnedByYou:
        print(f"Express bucket already exists: {express_bucket}")
        return express_bucket


def create_session(bucket_name: str, mode: str = "ReadWrite"):
    """Create a session for reduced authentication overhead."""
    response = s3.create_session(Bucket=bucket_name, SessionMode=mode)
    return response["Credentials"]


def upload_with_low_latency(bucket: str, key: str, data: bytes):
    """Upload data with minimum latency."""
    import time

    start = time.perf_counter()
    s3.put_object(Bucket=bucket, Key=key, Body=data)
    elapsed = (time.perf_counter() - start) * 1000
    print(f"Upload latency: {elapsed:.2f}ms")


def download_with_low_latency(bucket: str, key: str) -> bytes:
    """Download data with minimum latency."""
    import time

    start = time.perf_counter()
    response = s3.get_object(Bucket=bucket, Key=key)
    data = response["Body"].read()
    elapsed = (time.perf_counter() - start) * 1000
    print(f"Download latency: {elapsed:.2f}ms")
    return data


def atomic_append(bucket: str, key: str, data: bytes):
    """
    Append data to an existing object atomically.

    Note: This uses a custom endpoint extension for atomic appends.
    """
    import requests

    url = f"{ENDPOINT}/{bucket}/{key}?append"
    response = requests.post(
        url,
        data=data,
        auth=(ACCESS_KEY, SECRET_KEY),
        headers={"Content-Type": "application/octet-stream"},
    )
    response.raise_for_status()
    print(f"Appended {len(data)} bytes to {key}")


def benchmark_express(bucket: str, iterations: int = 100):
    """Benchmark Express bucket performance."""
    import time
    import statistics

    # Generate test data
    data = b"x" * 4096  # 4KB

    latencies = []
    for i in range(iterations):
        key = f"benchmark/test-{i}.bin"
        start = time.perf_counter()
        s3.put_object(Bucket=bucket, Key=key, Body=data)
        latencies.append((time.perf_counter() - start) * 1000)

    print(f"\nBenchmark Results ({iterations} iterations):")
    print(f"  Min latency:    {min(latencies):.2f}ms")
    print(f"  Max latency:    {max(latencies):.2f}ms")
    print(f"  Avg latency:    {statistics.mean(latencies):.2f}ms")
    print(f"  P50 latency:    {statistics.median(latencies):.2f}ms")
    print(f"  P99 latency:    {sorted(latencies)[int(iterations * 0.99)]:.2f}ms")


def main():
    print("S3 Express One Zone Example")
    print("=" * 50)

    # Create Express bucket
    bucket = create_express_bucket("ml-data")

    # Upload with low latency
    print("\n1. Low-latency upload:")
    upload_with_low_latency(bucket, "data/sample.bin", b"Hello, Express!")

    # Download with low latency
    print("\n2. Low-latency download:")
    data = download_with_low_latency(bucket, "data/sample.bin")
    print(f"   Downloaded: {data.decode()}")

    # Atomic append for streaming data
    print("\n3. Atomic append (streaming log):")
    for i in range(5):
        atomic_append(bucket, "logs/app.log", f"Log entry {i}\n".encode())

    # Performance benchmark
    print("\n4. Performance benchmark:")
    benchmark_express(bucket, iterations=50)

    print("\nDone!")


if __name__ == "__main__":
    main()
