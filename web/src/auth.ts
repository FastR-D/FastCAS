import { initColorMode } from "@fastrd/ui-foundation";
import "@fastrd/ui-react/styles.css";
import "./auth.css";

// Authentication remains a native, server-validated form, including without JavaScript.
const theme = initColorMode({ storageKey: "fastcas.color-mode" });
const select = document.querySelector<HTMLSelectElement>("[data-theme-select]");
if (select) {
  select.value = theme.mode;
  select.addEventListener("change", () => {
    if (
      select.value === "system" ||
      select.value === "light" ||
      select.value === "dark"
    )
      theme.setMode(select.value);
  });
  theme.subscribe((state) => {
    select.value = state.mode;
  });
}
