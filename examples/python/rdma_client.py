#!/usr/bin/env python3
"""
S3 over RDMA Client Example

Demonstrates ultra-low latency object access via InfiniBand/RoCE.

Note: This requires RDMA hardware and drivers to be installed.
For systems without RDMA, this serves as a reference implementation.
"""

import os
import time
import statistics
from typing import Optional

# Configuration
RDMA_ENDPOINT = os.getenv("NEBULAIO_RDMA_ENDPOINT", "192.168.100.1:9100")
S3_ENDPOINT = os.getenv("NEBULAIO_ENDPOINT", "http://localhost:9000")
ACCESS_KEY = os.getenv("NEBULAIO_ACCESS_KEY", "minioadmin")
SECRET_KEY = os.getenv("NEBULAIO_SECRET_KEY", "minioadmin")


class RDMAClient:
    """
    RDMA client for NebulaIO.

    In a real implementation, this would use libibverbs directly.
    This is a simulation that shows the expected API.
    """

    def __init__(
        self,
        endpoint: str,
        access_key: str,
        secret_key: str,
        device_name: str = "mlx5_0",
        ib_port: int = 1,
    ):
        self.endpoint = endpoint
        self.access_key = access_key
        self.secret_key = secret_key
        self.device_name = device_name
        self.ib_port = ib_port
        self.connected = False
        self._simulated = True  # Set to False when real RDMA is available

    def connect(self):
        """Establish RDMA connection."""
        if self._simulated:
            print(f"[Simulated] Connecting to {self.endpoint}...")
            print(f"[Simulated] Device: {self.device_name}, Port: {self.ib_port}")
            self.connected = True
            return

        # Real implementation would:
        # 1. Open RDMA device
        # 2. Allocate protection domain
        # 3. Create completion queues
        # 4. Create queue pair
        # 5. Exchange connection info with server
        # 6. Transition QP to RTS state
        raise NotImplementedError("Real RDMA requires libibverbs")

    def disconnect(self):
        """Close RDMA connection."""
        if self._simulated:
            print("[Simulated] Disconnecting...")
            self.connected = False
            return

        # Real implementation would clean up resources
        raise NotImplementedError("Real RDMA requires libibverbs")

    def register_memory(self, size: int) -> "MemoryRegion":
        """Register memory for RDMA operations."""
        if self._simulated:
            return SimulatedMemoryRegion(size)

        # Real implementation would use ibv_reg_mr
        raise NotImplementedError("Real RDMA requires libibverbs")

    def get_object(self, bucket: str, key: str, mr: "MemoryRegion" = None) -> bytes:
        """
        Read object using RDMA.

        If mr is provided, data is read directly into the registered memory.
        """
        if self._simulated:
            # Simulate RDMA read with timing
            import boto3

            s3 = boto3.client(
                "s3",
                endpoint_url=S3_ENDPOINT,
                aws_access_key_id=self.access_key,
                aws_secret_access_key=self.secret_key,
            )
            start = time.perf_counter()
            response = s3.get_object(Bucket=bucket, Key=key)
            data = response["Body"].read()
            elapsed_us = (time.perf_counter() - start) * 1_000_000

            # RDMA would be 10-20x faster
            simulated_latency_us = elapsed_us / 15
            print(f"[Simulated RDMA] GET latency: {simulated_latency_us:.1f}μs")

            if mr:
                mr.write(data)
            return data

        raise NotImplementedError("Real RDMA requires libibverbs")

    def put_object(
        self, bucket: str, key: str, data: bytes, mr: "MemoryRegion" = None
    ) -> None:
        """
        Write object using RDMA.

        If mr is provided, data is read from the registered memory.
        """
        if self._simulated:
            import boto3

            s3 = boto3.client(
                "s3",
                endpoint_url=S3_ENDPOINT,
                aws_access_key_id=self.access_key,
                aws_secret_access_key=self.secret_key,
            )
            start = time.perf_counter()
            s3.put_object(Bucket=bucket, Key=key, Body=data)
            elapsed_us = (time.perf_counter() - start) * 1_000_000

            simulated_latency_us = elapsed_us / 15
            print(f"[Simulated RDMA] PUT latency: {simulated_latency_us:.1f}μs")
            return

        raise NotImplementedError("Real RDMA requires libibverbs")


class SimulatedMemoryRegion:
    """Simulated RDMA memory region."""

    def __init__(self, size: int):
        self.size = size
        self.buffer = bytearray(size)
        self.lkey = 12345  # Simulated local key
        self.rkey = 67890  # Simulated remote key

    def write(self, data: bytes, offset: int = 0):
        """Write data to the memory region."""
        self.buffer[offset : offset + len(data)] = data

    def read(self, size: int = None, offset: int = 0) -> bytes:
        """Read data from the memory region."""
        if size is None:
            size = self.size - offset
        return bytes(self.buffer[offset : offset + size])


def benchmark_rdma(client: RDMAClient, bucket: str, iterations: int = 100):
    """Benchmark RDMA operations."""
    import boto3

    # Create test bucket and data
    s3 = boto3.client(
        "s3",
        endpoint_url=S3_ENDPOINT,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
    )

    try:
        s3.create_bucket(Bucket=bucket)
    except:
        pass

    # Upload test objects
    data = b"x" * 4096  # 4KB
    for i in range(iterations):
        s3.put_object(Bucket=bucket, Key=f"bench/test-{i}.bin", Body=data)

    # Register memory
    mr = client.register_memory(4096)

    # Benchmark reads
    print(f"\nBenchmarking RDMA reads ({iterations} iterations)...")
    latencies = []

    for i in range(iterations):
        start = time.perf_counter()
        client.get_object(bucket, f"bench/test-{i}.bin", mr)
        latencies.append((time.perf_counter() - start) * 1_000_000)  # Convert to μs

    print(f"\nRDMA Read Benchmark Results:")
    print(f"  Min latency:  {min(latencies):.1f}μs")
    print(f"  Max latency:  {max(latencies):.1f}μs")
    print(f"  Avg latency:  {statistics.mean(latencies):.1f}μs")
    print(f"  P50 latency:  {statistics.median(latencies):.1f}μs")
    print(f"  P99 latency:  {sorted(latencies)[int(iterations * 0.99)]:.1f}μs")


def main():
    print("S3 over RDMA Client Example")
    print("=" * 50)

    print("\nNote: This is a simulated RDMA client.")
    print("Real RDMA requires InfiniBand/RoCE hardware and libibverbs.\n")

    # Create client
    client = RDMAClient(
        endpoint=RDMA_ENDPOINT,
        access_key=ACCESS_KEY,
        secret_key=SECRET_KEY,
    )

    # Connect
    print("1. Connecting...")
    client.connect()

    # Create test bucket
    bucket = "rdma-demo"
    import boto3

    s3 = boto3.client(
        "s3",
        endpoint_url=S3_ENDPOINT,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
    )
    try:
        s3.create_bucket(Bucket=bucket)
    except:
        pass

    # PUT operation
    print("\n2. PUT operation:")
    client.put_object(bucket, "test/data.bin", b"Hello, RDMA!")

    # GET operation
    print("\n3. GET operation:")
    data = client.get_object(bucket, "test/data.bin")
    print(f"   Retrieved: {data.decode()}")

    # Zero-copy with memory region
    print("\n4. Zero-copy with registered memory:")
    mr = client.register_memory(1024)
    client.get_object(bucket, "test/data.bin", mr)
    print(f"   Data in MR: {mr.read(12).decode()}")

    # Benchmark
    print("\n5. Performance benchmark:")
    benchmark_rdma(client, bucket, iterations=20)

    # Disconnect
    print("\n6. Disconnecting...")
    client.disconnect()

    print("\nDone!")
    print("\nFor real RDMA implementation:")
    print("  - Install rdma-core and libibverbs")
    print("  - Configure InfiniBand or RoCE v2")
    print("  - Use pyverbs for Python bindings")


if __name__ == "__main__":
    main()
