import { mount } from "svelte";
import SettingsPage from "../components/SettingsPage.svelte";

const app = document.querySelector<HTMLDivElement>("#app")!;
const params = new URLSearchParams(location.search);
const initialTab = params.get("tab") || "general";

mount(SettingsPage, {
  target: app,
  props: { initialTab },
});
