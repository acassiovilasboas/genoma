"""
Genoma Embeddings Service
Minimal HTTP API for generating text embeddings using sentence-transformers.
Model: all-MiniLM-L6-v2 (384 dimensions)
"""

import json
import logging
from flask import Flask, request, jsonify
from sentence_transformers import SentenceTransformer

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("genoma-embeddings")

app = Flask(__name__)
model = SentenceTransformer("all-MiniLM-L6-v2")

logger.info("🧬 Genoma Embeddings Service ready (model: all-MiniLM-L6-v2, dims: 384)")


@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "healthy", "model": "all-MiniLM-L6-v2", "dimensions": 384})


@app.route("/embed", methods=["POST"])
def embed():
    data = request.get_json()
    if not data or "texts" not in data:
        return jsonify({"error": "missing 'texts' field"}), 400

    texts = data["texts"]
    if not isinstance(texts, list) or len(texts) == 0:
        return jsonify({"error": "'texts' must be a non-empty list"}), 400

    if len(texts) > 100:
        return jsonify({"error": "max 100 texts per request"}), 400

    logger.info(f"Embedding {len(texts)} texts")
    embeddings = model.encode(texts, normalize_embeddings=True)

    return jsonify({
        "embeddings": embeddings.tolist(),
        "dimensions": 384,
        "count": len(texts),
    })


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5050)
