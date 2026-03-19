// Axiom GUI Dashboard - React entry point.
// Mounts the root App component into the DOM.
// In a Wails v2 app, the Wails runtime is available on window
// and Go backend bindings are accessible via window.go.main.App.
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Root element not found");
}

ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
