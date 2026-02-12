#!/usr/bin/env python3
"""
Demo: Semantic Store with Real Embedding

This script demonstrates the Semantic Store functionality with a real embedding model.
Requires embedding to be configured (e.g., Ollama or OpenAI).

Usage:
    # With Ollama
    export SOLOQUEUE_EMBEDDING_ENABLED=true
    export SOLOQUEUE_EMBEDDING_PROVIDER=ollama
    export SOLOQUEUE_EMBEDDING_MODEL=nomic-embed-text
    export SOLOQUEUE_EMBEDDING_API_BASE=http://localhost:11434/v1
    export SOLOQUEUE_EMBEDDING_DIMENSION=768
    
    python examples/semantic_store_demo.py
"""

import sys
import os

# Add src to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'src'))

from soloqueue.core.embedding import is_embedding_available, get_embedding_dimension
from soloqueue.core.memory.semantic_store import SemanticStore


def main():
    print("=" * 70)
    print("  Semantic Store Demo - Vector-based Knowledge Retrieval")
    print("=" * 70)
    print()
    
    # Check embedding availability
    if not is_embedding_available():
        print("❌ ERROR: Embedding model not configured!")
        print()
        print("Please configure embedding via environment variables:")
        print("  export SOLOQUEUE_EMBEDDING_ENABLED=true")
        print("  export SOLOQUEUE_EMBEDDING_PROVIDER=ollama")
        print("  export SOLOQUEUE_EMBEDDING_MODEL=nomic-embed-text")
        print("  export SOLOQUEUE_EMBEDDING_API_BASE=http://localhost:11434/v1")
        print("  export SOLOQUEUE_EMBEDDING_DIMENSION=768")
        print()
        print("Or use OpenAI:")
        print("  export SOLOQUEUE_EMBEDDING_ENABLED=true")
        print("  export SOLOQUEUE_EMBEDDING_PROVIDER=openai")
        print("  export SOLOQUEUE_EMBEDDING_MODEL=text-embedding-3-small")
        print("  export OPENAI_API_KEY=sk-...")
        return 1
    
    print(f"✅ Embedding enabled (dimension={get_embedding_dimension()})")
    print()
    
    # Create or load store
    storage_path = "/tmp/semantic_demo"
    print(f"📁 Storage: {storage_path}")
    
    store = SemanticStore(storage_path)
    existing_count = store.count()
    
    if existing_count > 0:
        print(f"📊 Found {existing_count} existing entries")
        print()
        
        # Ask if want to reset
        response = input("Reset and start fresh? (y/n): ")
        if response.lower() == 'y':
            store.reset()
            print("✅ Store reset")
            print()
    
    # Add knowledge entries
    print("=" * 70)
    print("  Adding Knowledge Entries")
    print("=" * 70)
    print()
    
    entries = [
        (
            "JWT authentication requires a secret key stored in environment variables. "
            "Never hardcode secrets in source code.",
            {"type": "best_practice", "topic": "security", "outcome": "success"}
        ),
        (
            "Database connection pooling with max_connections=10 prevents resource exhaustion. "
            "Monitor active connections and adjust based on load.",
            {"type": "lesson", "topic": "database", "outcome": "success"}
        ),
        (
            "Using global variables in multi-threaded code causes race conditions. "
            "Always use threading.Lock() or thread-local storage.",
            {"type": "gotcha", "topic": "concurrency", "outcome": "failure"}
        ),
        (
            "API rate limits can be handled with exponential backoff. "
            "Retry with delays: 1s, 2s, 4s, 8s, 16s.",
            {"type": "pattern", "topic": "api", "outcome": "success"}
        ),
        (
            "SQL injection can be prevented by using parameterized queries. "
            "Never concatenate user input directly into SQL strings.",
            {"type": "security", "topic": "database", "outcome": "critical"}
        )
    ]
    
    print(f"Adding {len(entries)} entries...")
    entry_ids = store.add_batch(entries)
    print(f"✅ Added {len(entry_ids)} entries")
    print()
    
    # Show stats
    stats = store.get_stats()
    print("📊 Store Statistics:")
    for key, value in stats.items():
        print(f"  {key}: {value}")
    print()
    
    # Semantic Search Examples
    print("=" * 70)
    print("  Semantic Search Examples")
    print("=" * 70)
    print()
    
    queries = [
        ("How to secure authentication?", None),
        ("Database optimization tips", {"type": "lesson"}),
        ("Common threading mistakes", None),
        ("Prevent security vulnerabilities", {"outcome": "critical"}),
    ]
    
    for query, filters in queries:
        print(f"🔍 Query: \"{query}\"")
        if filters:
            print(f"   Filters: {filters}")
        
        results = store.search(query, top_k=2, filter_metadata=filters)
        
        if results:
            for i, result in enumerate(results, 1):
                print(f"\n   {i}. Score: {result.score:.3f}")
                print(f"      Content: {result.content[:80]}...")
                print(f"      Topic: {result.metadata.get('topic')}")
                print(f"      Type: {result.metadata.get('type')}")
        else:
            print("   No results found")
        
        print()
    
    # Direct ID retrieval
    print("=" * 70)
    print("  Direct ID Retrieval")
    print("=" * 70)
    print()
    
    if entry_ids:
        test_id = entry_ids[0]
        print(f"Retrieving entry: {test_id}")
        entry = store.get_by_id(test_id)
        if entry:
            print(f"  Content: {entry.content}")
            print(f"  Metadata: {entry.metadata}")
        print()
    
    # Summary
    print("=" * 70)
    print("  Demo Complete!")
    print("=" * 70)
    print()
    print(f"✅ Total entries in store: {store.count()}")
    print(f"📁 Storage location: {storage_path}")
    print()
    print("The store is persistent. Run this script again to see the same data!")
    print()
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
