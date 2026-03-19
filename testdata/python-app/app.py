"""Simple Flask application with a health endpoint."""

from flask import Flask, jsonify

app = Flask(__name__)


@app.route("/")
def index():
    """Root endpoint."""
    return jsonify({"message": "Welcome"})


@app.route("/health")
def health():
    """Health check endpoint."""
    return jsonify({"status": "ok"})


if __name__ == "__main__":
    app.run(debug=True, port=5000)
