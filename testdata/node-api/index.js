const express = require("express");

const app = express();
app.use(express.json());

// In-memory user store.
const users = [
  { id: 1, name: "Alice", email: "alice@example.com" },
  { id: 2, name: "Bob", email: "bob@example.com" },
];

// GET / -- Root endpoint.
app.get("/", (req, res) => {
  res.json({ message: "Welcome to the API" });
});

// GET /health -- Health check.
app.get("/health", (req, res) => {
  res.json({ status: "ok" });
});

// GET /users -- List all users.
app.get("/users", (req, res) => {
  res.json(users);
});

// Only start listening when run directly (not when imported for tests).
if (require.main === module) {
  const port = process.env.PORT || 3000;
  app.listen(port, () => {
    console.log(`Server running on port ${port}`);
  });
}

module.exports = app;
