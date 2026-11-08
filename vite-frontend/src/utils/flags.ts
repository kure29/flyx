// Windows has no flag emoji: Chrome and Edge show the two letters instead
// (🇭🇰 becomes "HK"). Where that happens, load a font with only the flags,
// bundled with the panel (no third-party request). The font is named first in the page's font stack (globals.css).
import { polyfillCountryFlagEmojis } from "country-flag-emoji-polyfill";
import flagFont from "country-flag-emoji-polyfill/dist/TwemojiCountryFlags.woff2?url";

export function loadFlagFont() {
  try {
    polyfillCountryFlagEmojis("Twemoji Country Flags", flagFont);
  } catch {
    // Detection failed: flags stay as the browser draws them.
  }
}
