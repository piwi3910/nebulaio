#!/usr/bin/env python3
"""
NVIDIA NIM Inference Example

Demonstrates running AI inference on objects stored in NebulaIO.
"""

import os
import json
import requests
import boto3
from typing import Optional, Iterator

# Configuration
NEBULAIO_ENDPOINT = os.getenv("NEBULAIO_ENDPOINT", "http://localhost:9000")
ADMIN_ENDPOINT = os.getenv("NEBULAIO_ADMIN_ENDPOINT", "http://localhost:9001")
ACCESS_KEY = os.getenv("NEBULAIO_ACCESS_KEY", "minioadmin")
SECRET_KEY = os.getenv("NEBULAIO_SECRET_KEY", "minioadmin")
NVIDIA_API_KEY = os.getenv("NVIDIA_API_KEY", "")


class NIMClient:
    """Client for NebulaIO NIM integration."""

    def __init__(
        self,
        nebulaio_endpoint: str,
        admin_endpoint: str,
        access_key: str,
        secret_key: str,
        nvidia_api_key: str = None,
    ):
        self.nebulaio_endpoint = nebulaio_endpoint
        self.admin_endpoint = admin_endpoint
        self.access_key = access_key
        self.secret_key = secret_key
        self.nvidia_api_key = nvidia_api_key

        # S3 client for object access
        self.s3 = boto3.client(
            "s3",
            endpoint_url=nebulaio_endpoint,
            aws_access_key_id=access_key,
            aws_secret_access_key=secret_key,
        )

    def infer(
        self,
        bucket: str,
        key: str,
        model: str,
        prompt: str,
        max_tokens: int = 200,
        temperature: float = 0.7,
    ) -> dict:
        """Run inference on a stored object."""
        response = requests.post(
            f"{self.admin_endpoint}/api/v1/nim/infer",
            json={
                "bucket": bucket,
                "key": key,
                "model": model,
                "prompt": prompt,
                "max_tokens": max_tokens,
                "temperature": temperature,
            },
            headers={
                "Authorization": f"Bearer {self.access_key}:{self.secret_key}",
                "X-NIM-API-Key": self.nvidia_api_key,
            },
        )
        response.raise_for_status()
        return response.json()

    def infer_stream(
        self,
        bucket: str,
        key: str,
        model: str,
        prompt: str,
        max_tokens: int = 500,
    ) -> Iterator[str]:
        """Stream inference response."""
        response = requests.post(
            f"{self.admin_endpoint}/api/v1/nim/infer",
            json={
                "bucket": bucket,
                "key": key,
                "model": model,
                "prompt": prompt,
                "max_tokens": max_tokens,
                "stream": True,
            },
            headers={
                "Authorization": f"Bearer {self.access_key}:{self.secret_key}",
                "X-NIM-API-Key": self.nvidia_api_key,
            },
            stream=True,
        )
        response.raise_for_status()

        for line in response.iter_lines():
            if line:
                data = line.decode("utf-8")
                if data.startswith("data: "):
                    chunk = json.loads(data[6:])
                    if "content" in chunk:
                        yield chunk["content"]

    def detect_objects(
        self, bucket: str, key: str, labels: list[str], model: str = "nvidia/grounding-dino"
    ) -> dict:
        """Run object detection on an image."""
        response = requests.post(
            f"{self.admin_endpoint}/api/v1/nim/infer",
            json={
                "bucket": bucket,
                "key": key,
                "model": model,
                "task": "detection",
                "labels": labels,
            },
            headers={
                "Authorization": f"Bearer {self.access_key}:{self.secret_key}",
                "X-NIM-API-Key": self.nvidia_api_key,
            },
        )
        response.raise_for_status()
        return response.json()

    def transcribe(
        self, bucket: str, key: str, model: str = "nvidia/parakeet-ctc-1.1b"
    ) -> dict:
        """Transcribe audio file."""
        response = requests.post(
            f"{self.admin_endpoint}/api/v1/nim/infer",
            json={
                "bucket": bucket,
                "key": key,
                "model": model,
                "task": "transcription",
            },
            headers={
                "Authorization": f"Bearer {self.access_key}:{self.secret_key}",
                "X-NIM-API-Key": self.nvidia_api_key,
            },
        )
        response.raise_for_status()
        return response.json()

    def embed(
        self, bucket: str, key: str, model: str = "nvidia/nv-embed-v2"
    ) -> list[float]:
        """Generate embeddings for text content."""
        response = requests.post(
            f"{self.admin_endpoint}/api/v1/nim/infer",
            json={
                "bucket": bucket,
                "key": key,
                "model": model,
                "task": "embedding",
            },
            headers={
                "Authorization": f"Bearer {self.access_key}:{self.secret_key}",
                "X-NIM-API-Key": self.nvidia_api_key,
            },
        )
        response.raise_for_status()
        return response.json().get("embedding", [])


def setup_test_data(client: NIMClient):
    """Create test data for inference examples."""
    bucket = "nim-demo"

    # Create bucket
    try:
        client.s3.create_bucket(Bucket=bucket)
    except:
        pass

    # Upload test document
    client.s3.put_object(
        Bucket=bucket,
        Key="documents/article.txt",
        Body="""
        Artificial Intelligence in Healthcare: A Revolution in Progress

        The integration of AI in healthcare has transformed how we approach
        diagnosis, treatment, and patient care. Machine learning algorithms
        can now analyze medical images with accuracy matching or exceeding
        human experts. Natural language processing enables extraction of
        insights from unstructured clinical notes.

        Key applications include:
        - Radiology: AI-assisted X-ray and MRI analysis
        - Pathology: Automated cell and tissue classification
        - Drug Discovery: Molecular modeling and compound screening
        - Clinical Decision Support: Evidence-based recommendations

        Challenges remain around data privacy, model interpretability, and
        regulatory approval. However, the potential benefits for patient
        outcomes are significant.
        """,
    )

    print(f"Created test data in bucket: {bucket}")
    return bucket


def text_inference_example(client: NIMClient, bucket: str):
    """Demonstrate text inference on a document."""
    print("\n1. Text Inference (Document Summary)")
    print("-" * 40)

    try:
        result = client.infer(
            bucket=bucket,
            key="documents/article.txt",
            model="meta/llama-3.1-8b-instruct",
            prompt="Summarize this article in 3 bullet points.",
            max_tokens=200,
        )
        print(f"Summary:\n{result.get('response', 'No response')}")
    except Exception as e:
        print(f"Error (NIM may not be configured): {e}")


def streaming_inference_example(client: NIMClient, bucket: str):
    """Demonstrate streaming inference."""
    print("\n2. Streaming Inference")
    print("-" * 40)

    try:
        print("Response: ", end="")
        for chunk in client.infer_stream(
            bucket=bucket,
            key="documents/article.txt",
            model="meta/llama-3.1-8b-instruct",
            prompt="What are the key challenges mentioned in this article?",
        ):
            print(chunk, end="", flush=True)
        print()
    except Exception as e:
        print(f"Error (NIM may not be configured): {e}")


def embedding_example(client: NIMClient, bucket: str):
    """Demonstrate text embedding generation."""
    print("\n3. Text Embeddings")
    print("-" * 40)

    try:
        embedding = client.embed(
            bucket=bucket,
            key="documents/article.txt",
            model="nvidia/nv-embed-v2",
        )
        print(f"Embedding dimensions: {len(embedding)}")
        print(f"First 5 values: {embedding[:5]}")
    except Exception as e:
        print(f"Error (NIM may not be configured): {e}")


def main():
    print("NVIDIA NIM Inference Example")
    print("=" * 50)

    if not NVIDIA_API_KEY:
        print("\nWarning: NVIDIA_API_KEY not set.")
        print("Get your API key from https://build.nvidia.com")
        print("Running with simulated responses...\n")

    # Create client
    client = NIMClient(
        nebulaio_endpoint=NEBULAIO_ENDPOINT,
        admin_endpoint=ADMIN_ENDPOINT,
        access_key=ACCESS_KEY,
        secret_key=SECRET_KEY,
        nvidia_api_key=NVIDIA_API_KEY,
    )

    # Setup test data
    bucket = setup_test_data(client)

    # Run examples
    text_inference_example(client, bucket)
    streaming_inference_example(client, bucket)
    embedding_example(client, bucket)

    print("\n" + "=" * 50)
    print("Available NIM tasks:")
    print("  - Text: completion, chat, summarization, Q&A")
    print("  - Vision: detection, classification, segmentation")
    print("  - Audio: transcription, diarization")
    print("  - Embedding: text embedding generation")
    print("\nDone!")


if __name__ == "__main__":
    main()
