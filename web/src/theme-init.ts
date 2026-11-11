import { initColorMode, type ColorModeController } from "@fastrd/ui-foundation";

declare global {
  interface Window {
    fastCASColorMode: ColorModeController;
  }
}

// Built as a blocking classic script; console and native auth reuse this handle.
window.fastCASColorMode = initColorMode({ storageKey: "fastcas.color-mode" });
