import { createElement } from "react";
import { createRoot } from "react-dom/client";
import { Guest, takeInitialCredential } from "./Guest";
import "@fastrd/ui-react/styles.css";
import "./auth.css";

// Authentication remains a native, server-validated form, including without JavaScript.
const theme = window.fastCASColorMode;
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

const registration = document.getElementById("registration-root");
if (registration) {
  const initial = takeInitialCredential();
  const tabs = Array.from(document.querySelectorAll<HTMLAnchorElement>("[data-auth-tab]"));
  const panels = Array.from(document.querySelectorAll<HTMLElement>("[data-auth-panel]"));
  function activate(name: string) {
    tabs.forEach((tab) => {
      const selected = tab.dataset.authTab === name;
      tab.setAttribute("aria-selected", String(selected));
      tab.tabIndex = selected ? 0 : -1;
    });
    panels.forEach((panel) => { panel.hidden = panel.dataset.authPanel !== name; });
  }
  tabs.forEach((tab, index) => {
    tab.addEventListener("click", (event) => {
      event.preventDefault();
      activate(tab.dataset.authTab!);
    });
    tab.addEventListener("keydown", (event) => {
      let next: number;
      if (event.key === "ArrowRight") next = (index + 1) % tabs.length;
      else if (event.key === "ArrowLeft") next = (index + tabs.length - 1) % tabs.length;
      else if (event.key === "Home") next = 0;
      else if (event.key === "End") next = tabs.length - 1;
      else return;
      event.preventDefault();
      activate(tabs[next].dataset.authTab!);
      tabs[next].focus();
    });
  });
  createRoot(registration).render(createElement(Guest, {
    initial,
    onLogin: () => { activate("login"); tabs[0].focus(); },
  }));
}
