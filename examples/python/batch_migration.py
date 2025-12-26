#!/usr/bin/env python3
"""
NebulaIO Batch Replication Example

Demonstrates bulk data migration and disaster recovery using
the Batch Replication API.
"""

import os
import time
import requests
from datetime import datetime, timedelta

# Configuration
ADMIN_ENDPOINT = os.getenv("NEBULAIO_ADMIN_ENDPOINT", "http://localhost:9001")
ADMIN_TOKEN = os.getenv("NEBULAIO_ADMIN_TOKEN", "")


class BatchReplicationClient:
    """Client for NebulaIO Batch Replication API."""

    def __init__(self, endpoint: str, token: str):
        self.endpoint = endpoint.rstrip("/")
        self.session = requests.Session()
        self.session.headers.update({
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        })

    def create_job(
        self,
        job_id: str,
        source_bucket: str,
        dest_bucket: str,
        description: str = "",
        prefix: str = None,
        dest_endpoint: str = None,
        dest_access_key: str = None,
        dest_secret_key: str = None,
        concurrency: int = 10,
        rate_limit_bps: int = None,
        min_size: int = None,
        max_size: int = None,
        created_after: str = None,
        created_before: str = None,
        tags: dict = None,
    ) -> dict:
        """Create a new batch replication job."""
        payload = {
            "job_id": job_id,
            "source_bucket": source_bucket,
            "destination_bucket": dest_bucket,
            "description": description,
            "concurrency": concurrency,
        }

        if prefix:
            payload["prefix"] = prefix
        if dest_endpoint:
            payload["destination_endpoint"] = dest_endpoint
        if dest_access_key:
            payload["destination_access_key"] = dest_access_key
        if dest_secret_key:
            payload["destination_secret_key"] = dest_secret_key
        if rate_limit_bps:
            payload["rate_limit_bytes_per_sec"] = rate_limit_bps
        if min_size:
            payload["min_size"] = min_size
        if max_size:
            payload["max_size"] = max_size
        if created_after:
            payload["created_after"] = created_after
        if created_before:
            payload["created_before"] = created_before
        if tags:
            payload["tags"] = tags

        response = self.session.post(
            f"{self.endpoint}/admin/batch/jobs",
            json=payload
        )
        response.raise_for_status()
        return response.json()

    def start_job(self, job_id: str) -> dict:
        """Start a batch replication job."""
        response = self.session.post(
            f"{self.endpoint}/admin/batch/jobs/{job_id}/start"
        )
        response.raise_for_status()
        return response.json()

    def get_job_status(self, job_id: str) -> dict:
        """Get the status of a batch replication job."""
        response = self.session.get(
            f"{self.endpoint}/admin/batch/jobs/{job_id}"
        )
        response.raise_for_status()
        return response.json()

    def list_jobs(self) -> list:
        """List all batch replication jobs."""
        response = self.session.get(f"{self.endpoint}/admin/batch/jobs")
        response.raise_for_status()
        return response.json()

    def pause_job(self, job_id: str) -> dict:
        """Pause a running job."""
        response = self.session.post(
            f"{self.endpoint}/admin/batch/jobs/{job_id}/pause"
        )
        response.raise_for_status()
        return response.json()

    def resume_job(self, job_id: str) -> dict:
        """Resume a paused job."""
        response = self.session.post(
            f"{self.endpoint}/admin/batch/jobs/{job_id}/resume"
        )
        response.raise_for_status()
        return response.json()

    def cancel_job(self, job_id: str) -> dict:
        """Cancel a job."""
        response = self.session.post(
            f"{self.endpoint}/admin/batch/jobs/{job_id}/cancel"
        )
        response.raise_for_status()
        return response.json()

    def delete_job(self, job_id: str) -> None:
        """Delete a job."""
        response = self.session.delete(
            f"{self.endpoint}/admin/batch/jobs/{job_id}"
        )
        response.raise_for_status()

    def get_failed_objects(self, job_id: str) -> list:
        """Get list of failed objects for a job."""
        response = self.session.get(
            f"{self.endpoint}/admin/batch/jobs/{job_id}/failed"
        )
        response.raise_for_status()
        return response.json()

    def retry_failed(self, job_id: str) -> dict:
        """Retry failed objects in a job."""
        response = self.session.post(
            f"{self.endpoint}/admin/batch/jobs/{job_id}/retry-failed"
        )
        response.raise_for_status()
        return response.json()


def format_bytes(size: int) -> str:
    """Format bytes as human-readable string."""
    for unit in ["B", "KB", "MB", "GB", "TB"]:
        if size < 1024.0:
            return f"{size:.1f} {unit}"
        size /= 1024.0
    return f"{size:.1f} PB"


def format_rate(bps: int) -> str:
    """Format bytes per second as human-readable string."""
    return f"{format_bytes(bps)}/s"


def monitor_job(client: BatchReplicationClient, job_id: str, interval: int = 5):
    """Monitor a job until completion."""
    print(f"\nMonitoring job: {job_id}")
    print("-" * 60)

    while True:
        status = client.get_job_status(job_id)
        progress = status.get("progress", {})

        total_obj = progress.get("total_objects", 0)
        done_obj = progress.get("completed_objects", 0)
        failed_obj = progress.get("failed_objects", 0)
        total_bytes = progress.get("total_bytes", 0)
        done_bytes = progress.get("completed_bytes", 0)
        rate = progress.get("current_rate_bytes_sec", 0)

        pct = (done_obj / total_obj * 100) if total_obj > 0 else 0
        eta = progress.get("estimated_completion", "calculating...")

        print(f"\r[{status['status'].upper():^10}] "
              f"Objects: {done_obj:,}/{total_obj:,} ({pct:.1f}%) | "
              f"Bytes: {format_bytes(done_bytes)}/{format_bytes(total_bytes)} | "
              f"Rate: {format_rate(rate)} | "
              f"Failed: {failed_obj} | "
              f"ETA: {eta}", end="", flush=True)

        if status["status"] in ["completed", "failed", "cancelled"]:
            print()
            break

        time.sleep(interval)

    return status


def example_local_bucket_copy():
    """Example: Copy between buckets on the same cluster."""
    client = BatchReplicationClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    job_id = f"local-copy-{datetime.now().strftime('%Y%m%d-%H%M%S')}"

    print("Creating local bucket copy job...")
    client.create_job(
        job_id=job_id,
        source_bucket="source-bucket",
        dest_bucket="backup-bucket",
        description="Local backup of source bucket",
        concurrency=20,
    )

    print("Starting job...")
    client.start_job(job_id)

    final_status = monitor_job(client, job_id)

    if final_status["status"] == "completed":
        print("\nMigration completed successfully!")
    else:
        print(f"\nMigration ended with status: {final_status['status']}")
        failed = client.get_failed_objects(job_id)
        if failed:
            print(f"Failed objects: {len(failed)}")
            for obj in failed[:5]:
                print(f"  - {obj['key']}: {obj['error']}")


def example_cross_cluster_dr():
    """Example: Cross-cluster disaster recovery replication."""
    client = BatchReplicationClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    job_id = f"dr-sync-{datetime.now().strftime('%Y%m%d-%H%M%S')}"

    print("Creating cross-cluster DR job...")
    client.create_job(
        job_id=job_id,
        source_bucket="critical-data",
        dest_bucket="critical-data",
        dest_endpoint="https://dr-site.example.com:9000",
        dest_access_key=os.getenv("DR_ACCESS_KEY"),
        dest_secret_key=os.getenv("DR_SECRET_KEY"),
        description="Cross-cluster disaster recovery sync",
        concurrency=10,
        rate_limit_bps=104857600,  # 100 MB/s to avoid saturating WAN
    )

    print("Starting job...")
    client.start_job(job_id)

    final_status = monitor_job(client, job_id)
    return final_status


def example_filtered_migration():
    """Example: Migrate only specific objects based on filters."""
    client = BatchReplicationClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    # Migrate only logs from 2024, larger than 1KB but smaller than 1GB
    yesterday = (datetime.now() - timedelta(days=1)).isoformat() + "Z"

    job_id = f"filtered-migrate-{datetime.now().strftime('%Y%m%d-%H%M%S')}"

    print("Creating filtered migration job...")
    client.create_job(
        job_id=job_id,
        source_bucket="logs",
        dest_bucket="logs-archive",
        prefix="2024/",
        min_size=1024,  # Skip files < 1KB
        max_size=1073741824,  # Skip files > 1GB
        created_before=yesterday,  # Only older files
        tags={"retention": "long-term"},  # Only tagged files
        description="Archive 2024 logs with retention tag",
        concurrency=15,
    )

    print("Starting job...")
    client.start_job(job_id)

    final_status = monitor_job(client, job_id)
    return final_status


def example_to_aws_s3():
    """Example: Migrate to AWS S3."""
    client = BatchReplicationClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    job_id = f"to-aws-{datetime.now().strftime('%Y%m%d-%H%M%S')}"

    print("Creating AWS S3 migration job...")
    client.create_job(
        job_id=job_id,
        source_bucket="local-data",
        dest_bucket="aws-backup-bucket",
        dest_endpoint="https://s3.us-east-1.amazonaws.com",
        dest_access_key=os.getenv("AWS_ACCESS_KEY_ID"),
        dest_secret_key=os.getenv("AWS_SECRET_ACCESS_KEY"),
        description="Migrate to AWS S3",
        concurrency=20,
    )

    print("Starting job...")
    client.start_job(job_id)

    final_status = monitor_job(client, job_id)
    return final_status


def main():
    import argparse

    parser = argparse.ArgumentParser(description="NebulaIO Batch Replication Example")
    subparsers = parser.add_subparsers(dest="command", help="Command to run")

    # Local copy
    local_parser = subparsers.add_parser("local-copy", help="Copy between local buckets")
    local_parser.add_argument("source", help="Source bucket")
    local_parser.add_argument("dest", help="Destination bucket")
    local_parser.add_argument("--prefix", default="", help="Object prefix filter")
    local_parser.add_argument("--concurrency", type=int, default=20, help="Worker concurrency")

    # Cross-cluster
    cross_parser = subparsers.add_parser("cross-cluster", help="Replicate to remote cluster")
    cross_parser.add_argument("source", help="Source bucket")
    cross_parser.add_argument("dest", help="Destination bucket")
    cross_parser.add_argument("--endpoint", required=True, help="Remote cluster endpoint")
    cross_parser.add_argument("--rate-limit", type=int, help="Rate limit in MB/s")

    # List jobs
    subparsers.add_parser("list", help="List all jobs")

    # Status
    status_parser = subparsers.add_parser("status", help="Get job status")
    status_parser.add_argument("job_id", help="Job ID")

    # Cancel
    cancel_parser = subparsers.add_parser("cancel", help="Cancel a job")
    cancel_parser.add_argument("job_id", help="Job ID")

    args = parser.parse_args()

    client = BatchReplicationClient(ADMIN_ENDPOINT, ADMIN_TOKEN)

    if args.command == "local-copy":
        job_id = f"local-copy-{datetime.now().strftime('%Y%m%d-%H%M%S')}"
        client.create_job(
            job_id=job_id,
            source_bucket=args.source,
            dest_bucket=args.dest,
            prefix=args.prefix,
            concurrency=args.concurrency,
        )
        client.start_job(job_id)
        monitor_job(client, job_id)

    elif args.command == "cross-cluster":
        job_id = f"cross-cluster-{datetime.now().strftime('%Y%m%d-%H%M%S')}"
        rate_limit = args.rate_limit * 1024 * 1024 if args.rate_limit else None
        client.create_job(
            job_id=job_id,
            source_bucket=args.source,
            dest_bucket=args.dest,
            dest_endpoint=args.endpoint,
            dest_access_key=os.getenv("DEST_ACCESS_KEY"),
            dest_secret_key=os.getenv("DEST_SECRET_KEY"),
            rate_limit_bps=rate_limit,
        )
        client.start_job(job_id)
        monitor_job(client, job_id)

    elif args.command == "list":
        jobs = client.list_jobs()
        print(f"{'Job ID':<30} {'Status':<12} {'Source':<20} {'Dest':<20}")
        print("-" * 82)
        for job in jobs:
            print(f"{job['job_id']:<30} {job['status']:<12} "
                  f"{job['source_bucket']:<20} {job['destination_bucket']:<20}")

    elif args.command == "status":
        status = client.get_job_status(args.job_id)
        print(f"Job: {status['job_id']}")
        print(f"Status: {status['status']}")
        print(f"Source: {status['source_bucket']}")
        print(f"Destination: {status['destination_bucket']}")
        if "progress" in status:
            p = status["progress"]
            print(f"Progress: {p['completed_objects']}/{p['total_objects']} objects")
            print(f"Bytes: {format_bytes(p['completed_bytes'])}/{format_bytes(p['total_bytes'])}")
            print(f"Rate: {format_rate(p['current_rate_bytes_sec'])}")
            print(f"Failed: {p['failed_objects']}")

    elif args.command == "cancel":
        client.cancel_job(args.job_id)
        print(f"Job {args.job_id} cancelled")

    else:
        parser.print_help()


if __name__ == "__main__":
    main()
