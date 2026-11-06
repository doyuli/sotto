import { mount } from "svelte";
import BubblePage from "../components/BubblePage.svelte";

const app = document.querySelector<HTMLDivElement>("#app")!;
mount(BubblePage, { target: app });
