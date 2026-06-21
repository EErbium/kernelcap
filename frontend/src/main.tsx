import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./index.css";

const _rootEl = document.getElementById("root");

createRoot(_rootEl!).render(
  <StrictMode>
    <App />
  </StrictMode>
);
