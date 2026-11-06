import "./style.css";
import { renderBubble, disposeBubble } from "./pages/bubble";
import { renderSettings } from "./pages/settings";
import { renderSnip, disposeSnip } from "./pages/snip";

const app = document.querySelector<HTMLDivElement>("#app")!;

function route(): void {
  disposeBubble();
  disposeSnip();
  const path = (location.hash || "#/").slice(1).split("?")[0] ?? "/";
  document.body.dataset.route = path.slice(1) || "root";
  app.replaceChildren();
  switch (path) {
    case "/bubble":
      void renderBubble(app);
      break;
    case "/settings":
      void renderSettings(app);
      break;
    case "/snip":
      renderSnip(app);
      break;
    default:
      break; // the window only opens the known routes
  }
}

route();
window.addEventListener("hashchange", route);
