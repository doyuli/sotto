import { mount } from "svelte";
import SnipPage from "../components/SnipPage.svelte";

const app = document.querySelector<HTMLDivElement>("#app")!;
mount(SnipPage, { target: app });
