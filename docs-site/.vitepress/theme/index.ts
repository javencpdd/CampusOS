import DefaultTheme from "vitepress/theme";
import type { Theme } from "vitepress";
import CampusHome from "./CampusHome.vue";
import "./custom.css";
import "./home.css";

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component("CampusHome", CampusHome);
  },
} satisfies Theme;
