const { describe, it } = require("node:test");
const assert = require("node:assert/strict");

// Basic smoke tests for the API module.
describe("Node API", () => {
  it("should export an express app", () => {
    const app = require("./index");
    assert.ok(app, "app should be defined");
    assert.equal(typeof app.listen, "function", "app should have a listen method");
  });

  it("should have route handlers registered", () => {
    const app = require("./index");
    // Express stores routes on the router stack.
    const routes = app._router.stack
      .filter((layer) => layer.route)
      .map((layer) => ({
        path: layer.route.path,
        methods: Object.keys(layer.route.methods),
      }));

    const paths = routes.map((r) => r.path);
    assert.ok(paths.includes("/"), "should have root route");
    assert.ok(paths.includes("/health"), "should have health route");
    assert.ok(paths.includes("/users"), "should have users route");
  });

  it("should have GET methods on all routes", () => {
    const app = require("./index");
    const routes = app._router.stack
      .filter((layer) => layer.route)
      .map((layer) => ({
        path: layer.route.path,
        methods: Object.keys(layer.route.methods),
      }));

    for (const route of routes) {
      assert.ok(
        route.methods.includes("get"),
        `${route.path} should support GET`
      );
    }
  });
});
