import { useSyncExternalStore } from "react";

// Match the existing lg: responsive styles; only one stats variant is mounted.
const media = window.matchMedia("(min-width: 64rem)");
const subscribe = (onChange: () => void) => {
  media.addEventListener("change", onChange);

  return () => media.removeEventListener("change", onChange);
};
const getSnapshot = () => media.matches;

export function useWideNodeLayout() {
  return useSyncExternalStore(subscribe, getSnapshot);
}
