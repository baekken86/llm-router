#!/usr/bin/env python3
"""Export benchmark data from ~/git/llm-benchmarks/benchmarks.db to models.json"""

import json
import sqlite3
from pathlib import Path

BENCHMARKS_DB = Path.home() / "git" / "llm-benchmarks" / "benchmarks.db"
OUTPUT = Path(__file__).parent.parent / "data" / "models.json"

FIELDS = {
    "intelligence": {
        "description": "AA Intelligence Index v4.1 score (0-100). Higher = more intelligent.",
        "source": "artificialanalysis.ai",
        "type": "number",
        "min": 0,
        "max": 100
    },
    "hallucination": {
        "description": "AA Omniscience Hallucination Rate (0-100). Lower = less hallucination.",
        "source": "artificialanalysis.ai",
        "type": "number",
        "min": 0,
        "max": 100
    },
    "coding": {
        "description": "AA Coding Index score (0-100). Higher = better at coding.",
        "source": "artificialanalysis.ai",
        "type": "number",
        "min": 0,
        "max": 100
    },
    "speed": {
        "description": "Output speed in tokens per second. Higher = faster.",
        "source": "artificialanalysis.ai",
        "type": "number",
        "unit": "tokens/sec"
    },
    "latency": {
        "description": "Time to first token in seconds. Lower = faster response.",
        "source": "artificialanalysis.ai",
        "type": "number",
        "unit": "seconds"
    },
    "context_window": {
        "description": "Maximum context window in tokens.",
        "type": "number"
    },
    "cost_type": {
        "description": "How the model is billed.",
        "type": "string",
        "values": ["subscription", "api-creds", "free"]
    },
    "cost_per_1m_input": {
        "description": "Cost per 1M input tokens in USD.",
        "type": "number",
        "unit": "USD"
    },
    "cost_per_1m_output": {
        "description": "Cost per 1M output tokens in USD.",
        "type": "number",
        "unit": "USD"
    },
    "cost_per_1m_cache": {
        "description": "Cost per 1M cached input tokens in USD.",
        "type": "number",
        "unit": "USD"
    },
    "has_reasoning_effort": {
        "description": "Whether the model supports configurable reasoning effort levels.",
        "type": "boolean"
    }
}

BENCHMARK_INDEXES = {
    "intelligence": "Intelligence Index",
    "hallucination": "AA-Omniscience Hallucination Rate",
    "coding": "Coding Index",
}

REASONING_KEYWORDS = ["high", "max", "low", "medium", "xhigh", "non-reasoning"]


def normalize_name(display_name: str) -> str:
    """Normalize model name for matching."""
    name = display_name.strip()
    
    # Remove effort level suffixes
    for kw in REASONING_KEYWORDS:
        if name.endswith(f"({kw})"):
            name = name[:-(len(kw)+2)].strip()
    
    # Map common names
    mapping = {
        "Claude Opus 4.6": "claude-opus-4-6",
        "Claude Sonnet 4.6": "claude-sonnet-4-6",
        "Claude Haiku 4.5": "claude-haiku-4-5",
        "Claude Haiku Latest": "claude-haiku-4-5",
        "Claude Sonnet Latest": "claude-sonnet-4-6",
        "Claude Opus 4": "claude-opus-4",
        "Claude Sonnet 4": "claude-sonnet-4",
        "Claude Sonnet 5": "claude-sonnet-5",
        "Claude Opus 4.5": "claude-opus-4-5",
        "Claude Opus 4.7": "claude-opus-4-7",
        "Claude Opus 4.8": "claude-opus-4-8",
        "Claude Fable 5": "claude-fable-5",
        "GPT-4o": "gpt-4o",
        "GPT-4o Mini": "gpt-4o-mini",
        "GPT-4.1": "gpt-4.1",
        "GPT-4.1 Mini": "gpt-4.1-mini",
        "GPT-4.1 Nano": "gpt-4.1-nano",
        "GPT-5.2": "gpt-5.2",
        "GPT-5.3": "gpt-5.3",
        "GPT-5.4": "gpt-5.4",
        "GPT-5.5": "gpt-5.5",
        "GPT-5.6 Sol": "gpt-5.6-sol",
        "GPT-5.6 Terra": "gpt-5.6-terra",
        "GPT-5.6 Luna": "gpt-5.6-luna",
        "o3": "o3",
        "o3 Mini": "o3-mini",
        "o3-pro": "o3-pro",
        "o4-mini": "o4-mini",
        "o4 Mini": "o4-mini",
        "DeepSeek V4 Pro": "deepseek-v4-pro",
        "DeepSeek V4 Flash": "deepseek-v4-flash",
        "DeepSeek R1": "deepseek-r1",
        "Qwen3.7 Max": "qwen3.7-max",
        "Qwen3.7 Plus": "qwen3.7-plus",
        "Qwen3.6 Plus": "qwen3.6-plus",
        "GLM-5.2": "glm-5.2",
        "GLM-5.1": "glm-5.1",
        "GLM-5": "glm-5",
        "Kimi K2.7 Code": "kimi-k2.7-code",
        "Kimi K2.6": "kimi-k2.6",
        "MiMo V2.5": "mimo-v2.5",
        "MiMo V2.5 Pro": "mimo-v2.5-pro",
        "MiniMax M3": "minimax-m3",
        "MiniMax M2.7": "minimax-m2.7",
        "Gemini 2.5 Pro": "gemini-2.5-pro",
        "Gemini 2.5 Flash": "gemini-2.5-flash",
        "Gemini 2.0 Flash": "gemini-2.0-flash",
    }
    
    return mapping.get(name, name.lower().replace(" ", "-").replace("_", "-"))


def detect_reasoning_effort(display_name: str) -> str | None:
    """Detect reasoning effort level from display name."""
    name = display_name.strip()
    for kw in REASONING_KEYWORDS:
        if name.endswith(f"({kw})"):
            return kw
    return None


def main():
    conn = sqlite3.connect(str(BENCHMARKS_DB))
    conn.row_factory = sqlite3.Row
    
    # Get all models with benchmark scores
    rows = conn.execute("""
        SELECT DISTINCT m.id, m.display_name
        FROM models m
        JOIN index_scores is2 ON is2.model_id = m.id
        ORDER BY m.display_name
    """).fetchall()
    
    print(f"Found {len(rows)} models with benchmark scores")
    
    # Group by normalized name
    models = {}
    for row in rows:
        model_id = row["id"]
        display_name = row["display_name"]
        normalized = normalize_name(display_name)
        effort = detect_reasoning_effort(display_name)
        
        if normalized not in models:
            models[normalized] = {
                "name": normalized,
                "display_names": [],
                "has_reasoning_effort": False,
                "efforts": {},
                "base": {}
            }
        
        models[normalized]["display_names"].append(display_name)
        
        # Get benchmark scores for this model
        scores = conn.execute("""
            SELECT bi.name, is2.score
            FROM index_scores is2
            JOIN benchmark_indexes bi ON bi.id = is2.index_id
            WHERE is2.model_id = ?
        """, [model_id]).fetchall()
        
        # Get pricing data
        pricing = conn.execute("""
            SELECT 
                p.input_price_per_million,
                p.output_price_per_million,
                p.cache_read_price_per_million,
                p.throughput_tps,
                p.latency_seconds
            FROM pricing p
            WHERE p.model_id = ?
            LIMIT 1
        """, [model_id]).fetchone()
        
        # Build metadata dict
        meta = {}
        for score in scores:
            idx_name = score["name"]
            if idx_name == BENCHMARK_INDEXES["intelligence"]:
                meta["intelligence"] = round(score["score"], 1)
            elif idx_name == BENCHMARK_INDEXES["hallucination"]:
                meta["hallucination"] = round(score["score"], 1)
            elif idx_name == BENCHMARK_INDEXES["coding"]:
                meta["coding"] = round(score["score"], 1)
        
        if pricing:
            if pricing["throughput_tps"]:
                meta["speed"] = round(pricing["throughput_tps"], 1)
            if pricing["latency_seconds"]:
                meta["latency"] = round(pricing["latency_seconds"], 2)
            if pricing["input_price_per_million"]:
                meta["cost_per_1m_input"] = pricing["input_price_per_million"]
            if pricing["output_price_per_million"]:
                meta["cost_per_1m_output"] = pricing["output_price_per_million"]
            if pricing["cache_read_price_per_million"]:
                meta["cost_per_1m_cache"] = pricing["cache_read_price_per_million"]
        
        if effort:
            models[normalized]["has_reasoning_effort"] = True
            models[normalized]["efforts"][effort] = meta
        else:
            models[normalized]["base"].update(meta)
    
    conn.close()
    
    # Build output
    output = {
        "fields": FIELDS,
        "models": []
    }
    
    for name, data in sorted(models.items()):
        model = {"name": name}
        
        # Add base metadata
        model.update(data["base"])
        
        if data["has_reasoning_effort"]:
            model["has_reasoning_effort"] = True
            model["efforts"] = data["efforts"]
        else:
            model["has_reasoning_effort"] = False
        
        output["models"].append(model)
    
    # Write output
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    with open(OUTPUT, "w") as f:
        json.dump(output, f, indent=2)
    
    print(f"Exported {len(output['models'])} models to {OUTPUT}")
    
    # Stats
    with_effort = sum(1 for m in output["models"] if m.get("has_reasoning_effort"))
    print(f"  {with_effort} models with reasoning effort")
    print(f"  {len(output['models']) - with_effort} models without reasoning effort")


if __name__ == "__main__":
    main()
