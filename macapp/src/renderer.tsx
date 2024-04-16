import App from "./components/app";
import "./styles/app.css";
import { createRoot } from "react-dom/client";

const container = document.getElementById("app");
const root = createRoot(container);
root.render(<App />);
